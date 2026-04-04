package api

import (
	"strings"
	"testing"

	"github.com/dnakitare/aether/pkg/api"
)

// --- ValidateBootArgs tests --------------------------------------------------

func TestValidateBootArgs_Empty(t *testing.T) {
	if err := ValidateBootArgs(""); err != nil {
		t.Errorf("empty boot args should be valid, got: %v", err)
	}
}

func TestValidateBootArgs_ValidKernelParams(t *testing.T) {
	cases := []string{
		"console=ttyS0 root=/dev/vda ro quiet",
		"console=ttyS0",
		"root=/dev/vda1 ro",
		"key=val1,val2",
		"addr=192.168.1.1:8080",
		"BOOT_IMAGE=/vmlinuz-5.15",
		"loglevel=3 rd.systemd.show_status=1",
	}
	for _, tc := range cases {
		if err := ValidateBootArgs(tc); err != nil {
			t.Errorf("valid args %q rejected: %v", tc, err)
		}
	}
}

func TestValidateBootArgs_DisallowedChars(t *testing.T) {
	cases := []struct {
		args string
		desc string
	}{
		{"console=ttyS0; rm -rf /", "semicolon injection"},
		{"key='value'", "single quote"},
		{"key=`cmd`", "backtick"},
		{"key=$(evil)", "dollar-paren"},
		{"key=value\x00null", "null byte"},
		{"key=value~tilde", "tilde"},
		{"key=value|pipe", "pipe"},
		{"key=value&background", "ampersand"},
		{"key=value>redirect", "redirect gt"},
		{"key=value<redirect", "redirect lt"},
		{"key=value!bang", "bang"},
		{"key=value#comment", "hash"},
		{"key=value$var", "dollar"},
		{"key=value%pct", "percent"},
		{"key=value^caret", "caret"},
		{"key=value*glob", "asterisk"},
		{"key=value?question", "question mark"},
		{"key=value[bracket", "open bracket"},
		{"key=value{brace", "open brace"},
		{`key=value\backslash`, "backslash"},
		{`key=value"quote`, "double quote"},
		{"key=value(paren", "open paren"},
	}
	for _, tc := range cases {
		if err := ValidateBootArgs(tc.args); err == nil {
			t.Errorf("args with %s %q should be rejected but was accepted", tc.desc, tc.args)
		}
	}
}

func TestValidateBootArgs_TooLong(t *testing.T) {
	args := strings.Repeat("a", 513)
	if err := ValidateBootArgs(args); err == nil {
		t.Error("boot args over 512 chars should be rejected")
	}
}

func TestValidateBootArgs_ExactMaxLength(t *testing.T) {
	args := strings.Repeat("a", 512)
	if err := ValidateBootArgs(args); err != nil {
		t.Errorf("512-char boot args should be valid, got: %v", err)
	}
}

// --- ValidateAgentID tests ---------------------------------------------------

func TestValidateAgentID_TooLong(t *testing.T) {
	id := api.AgentID(strings.Repeat("a", 65))
	if err := ValidateAgentID(id); err == nil {
		t.Error("agent ID over 64 chars should be rejected")
	}
}

func TestValidateAgentID_ExactMaxLength(t *testing.T) {
	id := api.AgentID(strings.Repeat("a", 64))
	if err := ValidateAgentID(id); err != nil {
		t.Errorf("64-char agent ID should be valid, got: %v", err)
	}
}

func TestValidateAgentID_Valid(t *testing.T) {
	cases := []string{"agent-1", "abc123", "a-b-c", "test", "my-agent-001"}
	for _, tc := range cases {
		if err := ValidateAgentID(api.AgentID(tc)); err != nil {
			t.Errorf("valid agent ID %q rejected: %v", tc, err)
		}
	}
}

func TestValidateAgentID_Invalid(t *testing.T) {
	cases := []struct {
		id   string
		desc string
	}{
		{"agent_id", "underscore"},
		{"agent id", "space"},
		{"AGENT.ID", "dot"},
		{"agent/path", "slash"},
		{"", "empty"},
		{"agent@id", "at-sign"},
	}
	for _, tc := range cases {
		if err := ValidateAgentID(api.AgentID(tc.id)); err == nil {
			t.Errorf("invalid agent ID %q (%s) should be rejected", tc.id, tc.desc)
		}
	}
}

func TestValidateAgentID_Empty(t *testing.T) {
	if err := ValidateAgentID(""); err == nil {
		t.Error("empty agent ID should be rejected")
	}
}
