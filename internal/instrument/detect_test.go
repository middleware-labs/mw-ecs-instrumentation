package instrument

import (
	"testing"
)

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantReg    string
		wantRepo   string
		wantTag    string
		wantIsECR  bool
		wantRegion string
	}{
		{
			name:       "ECR with tag",
			uri:        "123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:latest",
			wantReg:    "123456789.dkr.ecr.us-east-1.amazonaws.com",
			wantRepo:   "my-app",
			wantTag:    "latest",
			wantIsECR:  true,
			wantRegion: "us-east-1",
		},
		{
			name:       "ECR multi-part region",
			uri:        "111222333.dkr.ecr.ap-southeast-2.amazonaws.com/backend:v2.1",
			wantReg:    "111222333.dkr.ecr.ap-southeast-2.amazonaws.com",
			wantRepo:   "backend",
			wantTag:    "v2.1",
			wantIsECR:  true,
			wantRegion: "ap-southeast-2",
		},
		{
			name:      "GHCR with org/repo",
			uri:       "ghcr.io/org/repo:tag",
			wantReg:   "ghcr.io",
			wantRepo:  "org/repo",
			wantTag:   "tag",
			wantIsECR: false,
		},
		{
			name:      "Docker Hub user/repo explicit",
			uri:       "myuser/myapp:v1",
			wantReg:   "registry-1.docker.io",
			wantRepo:  "myuser/myapp",
			wantTag:   "v1",
			wantIsECR: false,
		},
		{
			name:      "Docker Hub bare image no tag",
			uri:       "nginx",
			wantReg:   "registry-1.docker.io",
			wantRepo:  "library/nginx",
			wantTag:   "latest",
			wantIsECR: false,
		},
		{
			name:      "Docker Hub library image with tag",
			uri:       "library/nginx:1.25",
			wantReg:   "registry-1.docker.io",
			wantRepo:  "library/nginx",
			wantTag:   "1.25",
			wantIsECR: false,
		},
		{
			name:      "Generic registry with port",
			uri:       "registry.example.com:5000/myrepo:stable",
			wantReg:   "registry.example.com:5000",
			wantRepo:  "myrepo",
			wantTag:   "stable",
			wantIsECR: false,
		},
		{
			name:      "Generic registry no tag",
			uri:       "registry.example.com/myrepo",
			wantReg:   "registry.example.com",
			wantRepo:  "myrepo",
			wantTag:   "latest",
			wantIsECR: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseImageRef(tc.uri)
			if got.registry != tc.wantReg {
				t.Errorf("registry: got %q, want %q", got.registry, tc.wantReg)
			}
			if got.repository != tc.wantRepo {
				t.Errorf("repository: got %q, want %q", got.repository, tc.wantRepo)
			}
			if got.tag != tc.wantTag {
				t.Errorf("tag: got %q, want %q", got.tag, tc.wantTag)
			}
			if got.isECR != tc.wantIsECR {
				t.Errorf("isECR: got %v, want %v", got.isECR, tc.wantIsECR)
			}
			if got.ecrRegion != tc.wantRegion {
				t.Errorf("ecrRegion: got %q, want %q", got.ecrRegion, tc.wantRegion)
			}
		})
	}
}

func TestClassifyConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  imageConfig
		want Language
	}{
		// Entrypoint/Cmd heuristics — Java
		{
			name: "java entrypoint bare",
			cfg:  cfgCmd([]string{"java", "-jar", "app.jar"}, nil, nil),
			want: LangJava,
		},
		{
			name: "java via path",
			cfg:  cfgCmd([]string{"/usr/lib/jvm/bin/java"}, nil, nil),
			want: LangJava,
		},
		{
			name: "java in cmd not entrypoint",
			cfg:  cfgCmd(nil, []string{"java", "-jar", "/app.jar"}, nil),
			want: LangJava,
		},

		// Node
		{
			name: "node entrypoint",
			cfg:  cfgCmd([]string{"node", "server.js"}, nil, nil),
			want: LangNode,
		},
		{
			name: "npm start via cmd",
			cfg:  cfgCmd(nil, []string{"npm", "start"}, nil),
			want: LangNode,
		},
		{
			name: "yarn",
			cfg:  cfgCmd([]string{"yarn", "run", "start"}, nil, nil),
			want: LangNode,
		},
		{
			name: "npx",
			cfg:  cfgCmd([]string{"npx", "ts-node", "src/index.ts"}, nil, nil),
			want: LangNode,
		},

		// Python
		{
			name: "python3 entrypoint",
			cfg:  cfgCmd([]string{"python3", "app.py"}, nil, nil),
			want: LangPython,
		},
		{
			name: "gunicorn",
			cfg:  cfgCmd([]string{"gunicorn", "app:create_app()"}, nil, nil),
			want: LangPython,
		},
		{
			name: "uvicorn",
			cfg:  cfgCmd([]string{"uvicorn", "main:app", "--host", "0.0.0.0"}, nil, nil),
			want: LangPython,
		},

		// Environment variable fallbacks
		{
			name: "JAVA_HOME env",
			cfg:  cfgCmd([]string{"/entrypoint.sh"}, nil, []string{"JAVA_HOME=/usr/lib/jvm/java-17"}),
			want: LangJava,
		},
		{
			name: "NODE_VERSION env",
			cfg:  cfgCmd([]string{"/start.sh"}, nil, []string{"NODE_VERSION=18.0.0"}),
			want: LangNode,
		},
		{
			name: "PYTHON_VERSION env",
			cfg:  cfgCmd(nil, []string{"/bin/sh"}, []string{"PYTHON_VERSION=3.11.0"}),
			want: LangPython,
		},
		{
			name: "PYTHON_PATH env",
			cfg:  cfgCmd(nil, []string{"/bin/sh"}, []string{"PYTHON_PATH=/usr/bin/python3"}),
			want: LangPython,
		},

		// Ambiguous / unknown
		{
			name: "nginx — unrecognised",
			cfg:  cfgCmd([]string{"nginx", "-g", "daemon off;"}, nil, nil),
			want: "",
		},
		{
			name: "empty config",
			cfg:  cfgCmd(nil, nil, nil),
			want: "",
		},

		// Java takes priority over env when both present
		{
			name: "java cmd beats NODE_VERSION env",
			cfg:  cfgCmd([]string{"java", "-jar", "app.jar"}, nil, []string{"NODE_VERSION=18"}),
			want: LangJava,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyConfig(tc.cfg)
			if got != tc.want {
				t.Errorf("classifyConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseBearerChallenge(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "standard docker hub challenge",
			input: `realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`,
			want: map[string]string{
				"realm":   "https://auth.docker.io/token",
				"service": "registry.docker.io",
				"scope":   "repository:library/nginx:pull",
			},
		},
		{
			name:  "minimal realm only",
			input: `realm="https://ghcr.io/token"`,
			want: map[string]string{
				"realm": "https://ghcr.io/token",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBearerChallenge(tc.input)
			for k, wantV := range tc.want {
				if got[k] != wantV {
					t.Errorf("key %q: got %q, want %q", k, got[k], wantV)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("len(got)=%d, len(want)=%d; got=%v", len(got), len(tc.want), got)
			}
		})
	}
}

func TestParseEnvMap(t *testing.T) {
	env := []string{
		"PATH=/usr/bin:/bin",
		"JAVA_HOME=/usr/lib/jvm/java-17",
		"NOVALUE",
		"EMPTY=",
	}
	m := parseEnvMap(env)
	if m["JAVA_HOME"] != "/usr/lib/jvm/java-17" {
		t.Errorf("JAVA_HOME: got %q", m["JAVA_HOME"])
	}
	if m["PATH"] != "/usr/bin:/bin" {
		t.Errorf("PATH: got %q", m["PATH"])
	}
	if m["EMPTY"] != "" {
		t.Errorf("EMPTY: expected empty string, got %q", m["EMPTY"])
	}
	// Key-only entry with no "=" should still appear as a key with empty value.
	if _, ok := m["NOVALUE"]; !ok {
		t.Error("NOVALUE key should be present")
	}
}

func TestParseImageRef_DockerIONormalization(t *testing.T) {
	tests := []struct {
		uri     string
		wantReg string
	}{
		{"docker.io/myuser/myapp:v1", "registry-1.docker.io"},
		{"index.docker.io/myuser/myapp:v1", "registry-1.docker.io"},
		{"myuser/myapp:v1", "registry-1.docker.io"},
	}
	for _, tc := range tests {
		got := parseImageRef(tc.uri)
		if got.registry != tc.wantReg {
			t.Errorf("parseImageRef(%q).registry = %q, want %q", tc.uri, got.registry, tc.wantReg)
		}
	}
}

func TestDetectLibC(t *testing.T) {
	tests := []struct {
		name string
		cfg  imageConfig
		ref  imageRef
		want LibC
	}{
		{
			name: "ALPINE_VERSION in env",
			cfg:  cfgCmd([]string{"node"}, nil, []string{"ALPINE_VERSION=3.18"}),
			ref:  imageRef{repository: "myapp", tag: "latest"},
			want: LibCMusl,
		},
		{
			name: "alpine in tag",
			cfg:  cfgCmd([]string{"node"}, nil, nil),
			ref:  imageRef{repository: "myapp", tag: "18-alpine"},
			want: LibCMusl,
		},
		{
			name: "alpine in repo name",
			cfg:  cfgCmd([]string{"python3"}, nil, nil),
			ref:  imageRef{repository: "myuser/python-alpine", tag: "3.11"},
			want: LibCMusl,
		},
		{
			name: "musl in tag",
			cfg:  cfgCmd([]string{"java"}, nil, nil),
			ref:  imageRef{repository: "myapp", tag: "latest-musl"},
			want: LibCMusl,
		},
		{
			name: "standard glibc image",
			cfg:  cfgCmd([]string{"node"}, nil, []string{"NODE_VERSION=18"}),
			ref:  imageRef{repository: "myuser/myapp", tag: "latest"},
			want: LibCGlibc,
		},
		{
			name: "empty config defaults to glibc",
			cfg:  cfgCmd(nil, nil, nil),
			ref:  imageRef{repository: "myapp", tag: "latest"},
			want: LibCGlibc,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectLibC(tc.cfg, tc.ref)
			if got != tc.want {
				t.Errorf("detectLibC() = %q, want %q", got, tc.want)
			}
		})
	}
}

// cfgCmd is a test helper that builds an imageConfig from raw slices.
func cfgCmd(entrypoint, cmd, env []string) imageConfig {
	var c imageConfig
	c.Config.Entrypoint = entrypoint
	c.Config.Cmd = cmd
	c.Config.Env = env
	return c
}
