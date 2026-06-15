package instrument

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// imageRef holds the parsed components of a container image URI.
type imageRef struct {
	registry   string // empty string means Docker Hub
	repository string
	tag        string
	isECR      bool
	ecrRegion  string
}

// imageConfig mirrors the subset of OCI/Docker image config JSON we care about.
type imageConfig struct {
	Config struct {
		Entrypoint []string          `json:"Entrypoint"`
		Cmd        []string          `json:"Cmd"`
		Env        []string          `json:"Env"`
	} `json:"config"`
}

// ociManifest covers both Docker V2 schema 2 and OCI image manifests.
type ociManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"config"`
}

// DetectLanguage inspects a container image's metadata and returns the most
// likely runtime language. It returns Language("") when detection is
// inconclusive so callers can fall back to prompting the user.
func DetectLanguage(ctx context.Context, imageURI string) (Language, LibC, error) {
	ref := parseImageRef(imageURI)

	var cfg imageConfig
	var err error

	if ref.isECR {
		cfg, err = fetchECRConfig(ctx, ref)
	} else {
		cfg, err = fetchOCIConfig(ctx, ref)
	}
	if err != nil {
		return "", "", fmt.Errorf("detect language for %q: %w", imageURI, err)
	}

	return classifyConfig(cfg), detectLibC(cfg, ref), nil
}

// parseImageRef splits a raw image URI into its components.
//
// Handled forms:
//
//	123456789.dkr.ecr.us-east-1.amazonaws.com/repo:tag   -> ECR
//	ghcr.io/org/repo:tag                                  -> generic registry
//	myuser/myapp:v1                                        -> Docker Hub, explicit repo
//	nginx                                                  -> Docker Hub, library image
func parseImageRef(uri string) imageRef {
	ref := imageRef{tag: "latest"}

	// Split off tag / digest.
	nameAndTag := uri
	if idx := strings.LastIndex(uri, ":"); idx != -1 {
		// Make sure the colon is not inside a registry host (e.g. localhost:5000/repo).
		afterColon := uri[idx+1:]
		if !strings.Contains(afterColon, "/") {
			ref.tag = afterColon
			nameAndTag = uri[:idx]
		}
	}

	parts := strings.SplitN(nameAndTag, "/", 2)

	// Determine whether the first segment is a registry host.
	// A registry host contains a dot or a colon (port), or is "localhost".
	isRegistryHost := strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost"

	if isRegistryHost {
		ref.registry = parts[0]
		if len(parts) == 2 {
			ref.repository = parts[1]
		}
		// Detect ECR: <account>.dkr.ecr.<region>.amazonaws.com
		if strings.Contains(ref.registry, ".dkr.ecr.") && strings.HasSuffix(ref.registry, ".amazonaws.com") {
			ref.isECR = true
			// Extract region from e.g. "123456789.dkr.ecr.us-east-1.amazonaws.com"
			segments := strings.Split(ref.registry, ".")
			// segments: [account, dkr, ecr, region-part1, region-part2, amazonaws, com]
			// Region starts after "ecr." and ends before ".amazonaws"
			ecrIdx := -1
			for i, s := range segments {
				if s == "ecr" {
					ecrIdx = i
					break
				}
			}
			if ecrIdx != -1 && ecrIdx+1 < len(segments) {
				regionParts := segments[ecrIdx+1:]
				// Drop the trailing "amazonaws" and "com"
				for len(regionParts) > 0 && (regionParts[len(regionParts)-1] == "com" || regionParts[len(regionParts)-1] == "amazonaws") {
					regionParts = regionParts[:len(regionParts)-1]
				}
				ref.ecrRegion = strings.Join(regionParts, ".")
			}
		}
	} else {
		// Docker Hub image.
		if len(parts) == 1 {
			// Bare name like "nginx" -> library/nginx
			ref.repository = "library/" + parts[0]
		} else {
			// user/repo form
			ref.repository = nameAndTag
		}
		ref.registry = "registry-1.docker.io"
	}

	// Normalize docker.io to the actual API endpoint.
	if ref.registry == "docker.io" || ref.registry == "index.docker.io" {
		ref.registry = "registry-1.docker.io"
	}

	return ref
}

// fetchECRConfig retrieves the image config blob from Amazon ECR.
func fetchECRConfig(ctx context.Context, ref imageRef) (imageConfig, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if ref.ecrRegion != "" {
		opts = append(opts, awsconfig.WithRegion(ref.ecrRegion))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return imageConfig{}, fmt.Errorf("loading AWS config: %w", err)
	}

	client := ecr.NewFromConfig(cfg)

	out, err := client.BatchGetImage(ctx, &ecr.BatchGetImageInput{
		RepositoryName: &ref.repository,
		ImageIds: []ecrtypes.ImageIdentifier{
			{ImageTag: &ref.tag},
		},
		AcceptedMediaTypes: []string{
			"application/vnd.docker.distribution.manifest.v2+json",
			"application/vnd.oci.image.manifest.v1+json",
		},
	})
	if err != nil {
		return imageConfig{}, fmt.Errorf("ECR BatchGetImage: %w", err)
	}
	if len(out.Images) == 0 {
		return imageConfig{}, fmt.Errorf("ECR: no images returned for %s:%s", ref.repository, ref.tag)
	}

	manifestJSON := ""
	if out.Images[0].ImageManifest != nil {
		manifestJSON = *out.Images[0].ImageManifest
	}

	var manifest ociManifest
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return imageConfig{}, fmt.Errorf("parsing ECR manifest: %w", err)
	}
	if manifest.Config.Digest == "" {
		return imageConfig{}, fmt.Errorf("ECR manifest has no config digest")
	}

	// ECR exposes the OCI Distribution HTTP API. Fetch the config blob via that
	// endpoint, authenticated with a short-lived ECR bearer token.
	token, registry, err := ecrAuthToken(ctx, client, ref)
	if err != nil {
		return imageConfig{}, err
	}

	return fetchBlobViaHTTP(ctx, registry, ref.repository, manifest.Config.Digest, token)
}

// ecrAuthToken obtains a Docker-compatible base64 auth token from ECR and
// returns it together with the registry hostname.
func ecrAuthToken(ctx context.Context, client *ecr.Client, ref imageRef) (token, registry string, err error) {
	authOut, err := client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", "", fmt.Errorf("ECR GetAuthorizationToken: %w", err)
	}
	if len(authOut.AuthorizationData) == 0 {
		return "", "", fmt.Errorf("ECR: no authorization data returned")
	}
	ad := authOut.AuthorizationData[0]
	tok := ""
	if ad.AuthorizationToken != nil {
		tok = *ad.AuthorizationToken
	}
	reg := ref.registry
	if ad.ProxyEndpoint != nil {
		// ProxyEndpoint is "https://<registry>"; strip the scheme.
		reg = strings.TrimPrefix(*ad.ProxyEndpoint, "https://")
	}
	return tok, reg, nil
}

// fetchOCIConfig retrieves the image config blob using the OCI Distribution
// HTTP API. Supports Docker Hub, GHCR, and any generic registry.
func fetchOCIConfig(ctx context.Context, ref imageRef) (imageConfig, error) {
	httpClient := &http.Client{Timeout: 15 * time.Second}

	token, err := ociAuthToken(ctx, httpClient, ref)
	if err != nil {
		// Proceed without a token (public images on some registries allow it).
		token = ""
	}

	_, configDigest, err := fetchOCIManifest(ctx, httpClient, ref, token)
	if err != nil {
		return imageConfig{}, err
	}

	return fetchBlobViaHTTP(ctx, ref.registry, ref.repository, configDigest, token)
}

// ociAuthToken obtains a Bearer token from the registry's auth service using
// the WWW-Authenticate challenge flow (unauthenticated GET → 401 → token).
func ociAuthToken(ctx context.Context, client *http.Client, ref imageRef) (string, error) {
	scheme := "https"
	if ref.registry == "localhost" || strings.HasPrefix(ref.registry, "localhost:") {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, ref.registry, ref.repository, ref.tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		// No auth needed or unexpected.
		return "", nil
	}

	// Parse: WWW-Authenticate: Bearer realm="...",service="...",scope="..."
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(wwwAuth, "Bearer ") {
		return "", nil
	}
	params := parseBearerChallenge(wwwAuth[len("Bearer "):])
	realm := params["realm"]
	if realm == "" {
		return "", nil
	}

	tokenURL := realm
	sep := "?"
	if service, ok := params["service"]; ok && service != "" {
		tokenURL += sep + "service=" + service
		sep = "&"
	}
	scope := params["scope"]
	if scope == "" {
		scope = fmt.Sprintf("repository:%s:pull", ref.repository)
	}
	tokenURL += sep + "scope=" + scope

	treq, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}

	// Try credentials from ~/.docker/config.json if available.
	if creds := lookupDockerCreds(ref.registry); creds != "" {
		if username, password := decodeDockerAuth(creds); username != "" {
			fmt.Fprintf(os.Stderr, "\033[36m➜\033[0m  Using credentials from ~/.docker/config.json for %s\n", ref.registry)
			treq.SetBasicAuth(username, password)
		}
	}

	tresp, err := client.Do(treq)
	if err != nil {
		return "", err
	}
	defer tresp.Body.Close()
	body, err := io.ReadAll(tresp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.Token != "" {
		return tokenResp.Token, nil
	}
	return tokenResp.AccessToken, nil
}

// fetchOCIManifest fetches the image manifest and returns the config blob digest.
func fetchOCIManifest(ctx context.Context, client *http.Client, ref imageRef, token string) (string, string, error) {
	scheme := "https"
	if ref.registry == "localhost" || strings.HasPrefix(ref.registry, "localhost:") {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, ref.registry, ref.repository, ref.tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	}, ", "))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetching manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("manifest fetch returned HTTP %d for %s/%s:%s", resp.StatusCode, ref.registry, ref.repository, ref.tag)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	var manifest ociManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", "", fmt.Errorf("parsing manifest JSON: %w", err)
	}
	if manifest.Config.Digest == "" {
		return "", "", fmt.Errorf("manifest has no config digest (schemaVersion=%d mediaType=%q)", manifest.SchemaVersion, manifest.MediaType)
	}
	return string(body), manifest.Config.Digest, nil
}

// fetchBlobViaHTTP downloads a blob (config layer) from the OCI Distribution
// endpoint and decodes it into an imageConfig.
func fetchBlobViaHTTP(ctx context.Context, registry, repository, digest, token string) (imageConfig, error) {
	scheme := "https"
	if registry == "localhost" || strings.HasPrefix(registry, "localhost:") {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", scheme, registry, repository, digest)

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Drop Authorization on cross-domain redirects (e.g. registry → CloudFront).
			// The redirect URL is pre-signed and doesn't need the bearer token.
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return imageConfig{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return imageConfig{}, fmt.Errorf("fetching config blob: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return imageConfig{}, fmt.Errorf("config blob fetch returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return imageConfig{}, err
	}

	var cfg imageConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return imageConfig{}, fmt.Errorf("parsing config blob JSON: %w", err)
	}
	return cfg, nil
}

// classifyConfig applies the detection heuristics against an image config.
// Priority: Entrypoint/Cmd keywords first, then environment variables.
func classifyConfig(cfg imageConfig) Language {
	// Combine Entrypoint and Cmd into a single token set for keyword scanning.
	var cmdTokens []string
	for _, arg := range cfg.Config.Entrypoint {
		cmdTokens = append(cmdTokens, tokenize(arg)...)
	}
	for _, arg := range cfg.Config.Cmd {
		cmdTokens = append(cmdTokens, tokenize(arg)...)
	}

	if matchesJava(cmdTokens) {
		return LangJava
	}
	if matchesNode(cmdTokens) {
		return LangNode
	}
	if matchesPython(cmdTokens) {
		return LangPython
	}

	// Fall through to environment variable heuristics.
	envMap := parseEnvMap(cfg.Config.Env)

	if _, ok := envMap["JAVA_HOME"]; ok {
		return LangJava
	}
	if _, ok := envMap["NODE_VERSION"]; ok {
		return LangNode
	}
	if _, ok := envMap["PYTHON_VERSION"]; ok {
		return LangPython
	}
	if _, ok := envMap["PYTHON_PATH"]; ok {
		return LangPython
	}

	return ""
}

// tokenize splits an argument string on path separators and spaces so that
// "/usr/bin/java -jar" yields ["usr", "bin", "java", "-jar"].
func tokenize(s string) []string {
	s = strings.ToLower(s)
	// Replace path separators and spaces uniformly.
	s = strings.NewReplacer("/", " ", "\\", " ").Replace(s)
	raw := strings.Fields(s)
	var result []string
	for _, tok := range raw {
		// Strip common flags like "--server", "-jar"; keep the bare word.
		result = append(result, tok)
		// Also add just the basename in case of paths like "usr bin java".
		if base := strings.TrimLeft(tok, "-"); base != tok {
			result = append(result, base)
		}
	}
	return result
}

func matchesJava(tokens []string) bool {
	for _, t := range tokens {
		switch t {
		case "java", "jar":
			return true
		}
	}
	return false
}

func matchesNode(tokens []string) bool {
	for _, t := range tokens {
		switch t {
		case "node", "npm", "yarn", "npx":
			return true
		}
	}
	return false
}

func matchesPython(tokens []string) bool {
	for _, t := range tokens {
		switch t {
		case "python", "python3", "python2",
			"gunicorn", "uvicorn", "flask", "django":
			return true
		}
	}
	return false
}

// parseEnvMap converts a slice of "KEY=value" strings to a map.
func parseEnvMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func detectLibC(cfg imageConfig, ref imageRef) LibC {
	envMap := parseEnvMap(cfg.Config.Env)
	if _, ok := envMap["ALPINE_VERSION"]; ok {
		return LibCMusl
	}

	imageStr := strings.ToLower(ref.repository + ":" + ref.tag)
	if strings.Contains(imageStr, "alpine") || strings.Contains(imageStr, "musl") {
		return LibCMusl
	}

	return LibCGlibc
}

func lookupDockerCreds(registry string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return ""
	}

	var dc struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &dc); err != nil {
		return ""
	}

	candidates := []string{
		registry,
		"https://" + registry,
		"https://" + registry + "/v1/",
	}

	if registry == "registry-1.docker.io" {
		candidates = append(candidates,
			"https://index.docker.io/v1/",
			"https://index.docker.io/v1",
			"index.docker.io",
			"docker.io",
		)
	}

	for _, key := range candidates {
		if auth, ok := dc.Auths[key]; ok && auth.Auth != "" {
			return auth.Auth
		}
	}

	return ""
}

func decodeDockerAuth(encoded string) (username, password string) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// parseBearerChallenge parses a Bearer WWW-Authenticate parameter string into
// a key/value map. Input example: `realm="https://auth.example.com",service="reg"`.
func parseBearerChallenge(s string) map[string]string {
	params := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		params[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return params
}
