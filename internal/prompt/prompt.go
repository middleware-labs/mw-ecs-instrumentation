package prompt

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/chzyer/readline"
)

const (
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"

	iconQuestion = "?"
	iconSelect   = "›"
)

type Prompter struct {
	rl      *readline.Instance
	sigChan chan os.Signal
}

func New() (*Prompter, error) {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "",
		InterruptPrompt: "^C",
		EOFPrompt:       "",
		Stdout:          os.Stderr,
		Stderr:          os.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("initializing readline: %w", err)
	}

	p := &Prompter{rl: rl, sigChan: make(chan os.Signal, 1)}
	signal.Notify(p.sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-p.sigChan
		rl.Close()
		os.Exit(130)
	}()

	return p, nil
}

func (p *Prompter) Close() {
	signal.Stop(p.sigChan)
	p.rl.Close()
}

func (p *Prompter) printAbove(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	p.rl.Terminal.Write([]byte(msg))
}

func (p *Prompter) AskYesNo(question string, defaultYes bool) bool {
	hint := "[y/N]"
	defaultAnswer := false
	if defaultYes {
		hint = "[Y/n]"
		defaultAnswer = true
	}

	p.printAbove("\n")
	prompt := fmt.Sprintf("  %s%s%s %s%s%s %s%s%s ", colorGreen, iconQuestion, colorReset, colorBold, question, colorReset, colorCyan, hint, colorReset)
	p.rl.SetPrompt(prompt)

	for {
		line, err := p.rl.Readline()
		if err != nil {
			return defaultAnswer
		}
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			return defaultAnswer
		}
		switch line {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			p.rl.SetPrompt("    Please enter y or n: ")
		}
	}
}

func (p *Prompter) AskChoice(question string, options []string) (int, string) {
	p.printAbove("\n  %s%s%s %s%s%s\n", colorGreen, iconQuestion, colorReset, colorBold, question, colorReset)
	for i, opt := range options {
		p.printAbove("    %s%s%s %d) %s\n", colorCyan, iconSelect, colorReset, i+1, opt)
	}

	prompt := fmt.Sprintf("    %sEnter choice [1-%d]:%s ", colorBold, len(options), colorReset)
	p.rl.SetPrompt(prompt)

	for {
		line, err := p.rl.Readline()
		if err != nil {
			return 0, options[0]
		}
		line = strings.TrimSpace(line)
		idx, err := strconv.Atoi(line)
		if err == nil && idx >= 1 && idx <= len(options) {
			return idx - 1, options[idx-1]
		}
		p.printAbove("    Invalid choice.\n")
		p.rl.SetPrompt(prompt)
	}
}

func (p *Prompter) AskString(question, defaultVal string) string {
	if defaultVal != "" {
		p.printAbove("\n  %s%s%s %s%s%s %s(default: %s)%s\n", colorGreen, iconQuestion, colorReset, colorBold, question, colorReset, colorCyan, defaultVal, colorReset)
	} else {
		p.printAbove("\n  %s%s%s %s%s%s\n", colorGreen, iconQuestion, colorReset, colorBold, question, colorReset)
	}

	p.rl.SetPrompt(fmt.Sprintf("    %s%s%s ", colorCyan, iconSelect, colorReset))
	line, err := p.rl.Readline()
	if err != nil {
		return defaultVal
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}
