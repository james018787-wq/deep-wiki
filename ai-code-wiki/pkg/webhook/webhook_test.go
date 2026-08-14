package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestParsePush_GitLabBranch(t *testing.T) {
	body := []byte(`{
		"object_kind": "push",
		"before": "95790bf891e76fee5e1747ab589903a6a1f80f22",
		"after": "da1560886d4f094c3e6c9a403a3b30d9c9e1c22",
		"ref": "refs/heads/main",
		"repository": {"git_http_url": "http://gitlab.example.com/group/repo.git"}
	}`)
	ev, err := ParsePush(body)
	if err != nil {
		t.Fatalf("ParsePush error: %v", err)
	}
	if ev.Provider != ProviderGitLab {
		t.Errorf("provider = %v, want gitlab", ev.Provider)
	}
	if ev.RepoURL != "http://gitlab.example.com/group/repo.git" {
		t.Errorf("RepoURL = %q", ev.RepoURL)
	}
	if ev.Branch != "main" {
		t.Errorf("Branch = %q, want main", ev.Branch)
	}
	if ev.BeforeCommit != "95790bf891e76fee5e1747ab589903a6a1f80f22" {
		t.Errorf("BeforeCommit = %q", ev.BeforeCommit)
	}
	if ev.AfterCommit != "da1560886d4f094c3e6c9a403a3b30d9c9e1c22" {
		t.Errorf("AfterCommit = %q", ev.AfterCommit)
	}
	if ev.IsTag || ev.IsDelete {
		t.Errorf("branch push should not be tag/delete: %+v", ev)
	}
}

func TestParsePush_GiteeBranch(t *testing.T) {
	body := []byte(`{
		"hook_name": "push_hooks",
		"before": "0000000000000000000000000000000000000000",
		"after": "3a4f5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a",
		"ref": "refs/heads/master",
		"repository": {"git_http_url": "https://gitee.com/group/repo.git"}
	}`)
	ev, err := ParsePush(body)
	if err != nil {
		t.Fatalf("ParsePush error: %v", err)
	}
	if ev.Provider != ProviderGitee {
		t.Errorf("provider = %v, want gitee", ev.Provider)
	}
	if ev.Branch != "master" {
		t.Errorf("Branch = %q, want master", ev.Branch)
	}
	if ev.IsTag {
		t.Errorf("branch push should not be tag")
	}
}

func TestParsePush_TagFilter(t *testing.T) {
	// GitLab tag push
	ev, err := ParsePush([]byte(`{
		"object_kind": "push",
		"before": "aaa", "after": "bbb",
		"ref": "refs/tags/v1.0.0",
		"repository": {"git_http_url": "http://gitlab.example.com/g/repo.git"}
	}`))
	if err != nil {
		t.Fatalf("ParsePush error: %v", err)
	}
	if !ev.IsTag {
		t.Errorf("refs/tags should mark IsTag")
	}

	// GitLab object_kind=tag_push
	ev, err = ParsePush([]byte(`{
		"object_kind": "tag_push",
		"before": "aaa", "after": "bbb",
		"ref": "refs/tags/v2.0.0",
		"repository": {"git_http_url": "http://gitlab.example.com/g/repo.git"}
	}`))
	if err != nil {
		t.Fatalf("ParsePush error: %v", err)
	}
	if !ev.IsTag {
		t.Errorf("tag_push should mark IsTag")
	}

	// Gitee tag push hooks
	ev, err = ParsePush([]byte(`{
		"hook_name": "tag_push_hooks",
		"before": "aaa", "after": "bbb",
		"ref": "refs/tags/v3.0.0",
		"repository": {"git_http_url": "https://gitee.com/g/repo.git"}
	}`))
	if err != nil {
		t.Fatalf("ParsePush error: %v", err)
	}
	if !ev.IsTag {
		t.Errorf("tag_push_hooks should mark IsTag")
	}
}

func TestParsePush_DeleteBranch(t *testing.T) {
	ev, err := ParsePush([]byte(`{
		"object_kind": "push",
		"before": "aaa", "after": "0000000000000000000000000000000000000000",
		"ref": "refs/heads/feature-x",
		"repository": {"git_http_url": "http://gitlab.example.com/g/repo.git"}
	}`))
	if err != nil {
		t.Fatalf("ParsePush error: %v", err)
	}
	if !ev.IsDelete {
		t.Errorf("delete branch should mark IsDelete")
	}
}

func TestParsePush_UnknownEvent(t *testing.T) {
	_, err := ParsePush([]byte(`{"object_kind": "issue", "hook_name": "note_hooks"}`))
	if err == nil {
		t.Fatalf("unknown event should return error")
	}
}

func TestParsePush_Empty(t *testing.T) {
	if _, err := ParsePush(nil); err == nil {
		t.Fatalf("empty body should return error")
	}
}

func TestVerifyToken(t *testing.T) {
	if !VerifyToken("my-secret", "my-secret") {
		t.Errorf("matching token should pass")
	}
	if VerifyToken("wrong", "my-secret") {
		t.Errorf("mismatch token should fail")
	}
	if !VerifyToken("anything", "") {
		t.Errorf("empty secret should skip verification")
	}
	if VerifyToken("", "my-secret") {
		t.Errorf("empty provided token should fail")
	}
}

func TestVerifyGiteeSignature(t *testing.T) {
	secret := "webhook-secret"
	timestamp := "1700000000"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "\n" + secret))
	sig := hex.EncodeToString(mac.Sum(nil))

	if !VerifyGiteeSignature(secret, timestamp, sig) {
		t.Errorf("valid gitee signature should pass")
	}
	if VerifyGiteeSignature(secret, timestamp, strings.Repeat("0", 64)) {
		t.Errorf("invalid signature should fail")
	}
	if VerifyGiteeSignature(secret, "", sig) {
		t.Errorf("missing timestamp should fail")
	}
	if !VerifyGiteeSignature("", timestamp, sig) {
		t.Errorf("empty secret should skip verification")
	}
}