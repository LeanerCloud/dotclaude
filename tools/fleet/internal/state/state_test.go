package state

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sampleLedger() *Ledger {
	return &Ledger{Slots: []Slot{{
		Name: "cc-impl", Repo: "cloud-commitments-platform", Role: "impl", Harness: "kimi",
		SessionID: "00000000-0000-4000-8000-000000000001", Pane: "w1:p1", Cwd: "~/x", HandoffPath: "~/h.md",
		Status: Active, LastSeq: 5, LastRevision: 2, LastProgress: time.Date(2026, 10, 17, 1, 2, 3, 0, time.UTC),
	}}}
}

func TestLedgerRoundTrip(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "slots.json")
	if err := WriteLedger(p, sampleLedger()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slots[0] != sampleLedger().Slots[0] {
		t.Fatalf("round trip mismatch: %+v", got.Slots[0])
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestLedgerValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Ledger)
		want   string
	}{
		{"bad status", func(l *Ledger) { l.Slots[0].Status = "running" }, "unknown status"},
		{"empty status", func(l *Ledger) { l.Slots[0].Status = "" }, "unknown status"},
		{"harness in name", func(l *Ledger) { l.Slots[0].Name = "Cc Impl" }, "invalid name"},
		{"dup", func(l *Ledger) { l.Slots = append(l.Slots, l.Slots[0]) }, "duplicate"},
		{"bad role", func(l *Ledger) { l.Slots[0].Role = "boss" }, "unknown role"},
		{"empty role", func(l *Ledger) { l.Slots[0].Role = "" }, "unknown role"},
		{"bad harness", func(l *Ledger) { l.Slots[0].Harness = "gemini" }, "unknown harness"},
		{"empty harness", func(l *Ledger) { l.Slots[0].Harness = "" }, "unknown harness"},
		{"no repo", func(l *Ledger) { l.Slots[0].Repo = "" }, "repo is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := sampleLedger()
			tt.mutate(l)
			p := filepath.Join(t.TempDir(), "slots.json")
			if err := WriteLedger(p, l); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("write: want %q, got %v", tt.want, err)
			}
			if _, err := os.Stat(p); err == nil {
				t.Fatal("invalid ledger was written")
			}
		})
	}
}

func TestReadLedgerRejects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown.json":  `{"slots":[],"extra":1}`,
		"trailing.json": `{"slots":[]} {}`,
		"garbage.json":  `{"slots":[`,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadLedger(p); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := ReadLedger(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("missing file must fail")
	}
}

// A crash between writing the temp file and the rename must leave the old ledger valid.
func TestCrashBeforeRenameKeepsOldFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "slots.json")
	if err := WriteLedger(p, sampleLedger()); err != nil {
		t.Fatal(err)
	}
	next := sampleLedger()
	next.Slots[0].Status = Retired
	data, _ := json.Marshal(next)
	tmp, err := writeTmp(p, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tmp) != dir {
		t.Fatalf("temp file %s not in target dir", tmp)
	}
	got, err := ReadLedger(p)
	if err != nil || got.Slots[0].Status != Active {
		t.Fatalf("old file damaged: %v %+v", err, got)
	}
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	if got, _ = ReadLedger(p); got.Slots[0].Status != Retired {
		t.Fatal("rename did not publish new content")
	}
}

func TestAtomicWriteLeavesNoTemp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	for i := 0; i < 3; i++ {
		if err := WriteFileAtomic(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "nodir", "f"), nil, 0o600); err == nil {
		t.Fatal("want error for missing dir")
	}
}

// Both fsyncs must run, the file's before the rename and the directory's after it.
func TestAtomicWriteFsyncOrder(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	origFile, origDir := syncFile, syncDir
	t.Cleanup(func() { syncFile, syncDir = origFile, origDir })
	var events []string
	content := func() string { b, _ := os.ReadFile(p); return string(b) }
	syncFile = func(f *os.File) error {
		events = append(events, "file:"+content())
		return origFile(f)
	}
	syncDir = func(d *os.File) error {
		events = append(events, "dir:"+content())
		return origDir(d)
	}
	if err := WriteFileAtomic(p, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "file:old,dir:new" {
		t.Fatalf("want file fsync before rename and dir fsync after, got %s", got)
	}
	syncFile = func(*os.File) error { return os.ErrInvalid }
	if err := WriteFileAtomic(p, []byte("x"), 0o600); err == nil || content() != "new" {
		t.Fatalf("file fsync failure must abort before rename: %v %q", err, content())
	}
	syncFile = origFile
	syncDir = func(*os.File) error { return os.ErrInvalid }
	if err := WriteFileAtomic(p, []byte("y"), 0o600); err == nil {
		t.Fatal("dir fsync failure must surface")
	}
}

const goodHandoff = `# cc-impl handoff
Updated: 2026-10-17T10:00:00Z
## Current item
LeanerCloud/cloud-commitments-platform#42, fix thing
## Worktree
/home/x/wt
## Branch
fix/thing
## PR
LeanerCloud/cloud-commitments-platform#43
## Head SHA
0123456789abcdef0123456789abcdef01234567
## Claims held
- issue LeanerCloud/cloud-commitments-platform#42
- claim-pr-cloud-commitments-platform-43.txt
## Next action
wait for CI
## Blockers
none
## Queue
- #44 after this
`

func TestHandoffParse(t *testing.T) {
	t.Parallel()
	h, err := ParseHandoff(goodHandoff)
	if err != nil {
		t.Fatal(err)
	}
	if h.Slot != "cc-impl" || h.PR == "" || h.Blockers != "" || len(h.Queue) != 1 || h.Idle() {
		t.Fatalf("bad parse: %+v", h)
	}
	want := []Claim{{ClaimIssue, "LeanerCloud/cloud-commitments-platform", 42}, {ClaimPR, "cloud-commitments-platform", 43}}
	if len(h.Claims) != 2 || h.Claims[0] != want[0] || h.Claims[1] != want[1] {
		t.Fatalf("claims: %+v", h.Claims)
	}
	out, err := h.Format()
	if err != nil {
		t.Fatal(err)
	}
	if out != goodHandoff {
		t.Fatalf("format round trip differs:\n%s", out)
	}
}

func TestHandoffIdle(t *testing.T) {
	t.Parallel()
	h := &Handoff{Slot: "cc-impl", Updated: time.Now(), NextAction: "await work"}
	out, err := h.Format()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseHandoff(out)
	if err != nil || !back.Idle() || len(back.Claims) != 0 {
		t.Fatalf("idle round trip: %v %+v", err, back)
	}
}

func TestHandoffCRLFParsesLikeLF(t *testing.T) {
	t.Parallel()
	lf, err := ParseHandoff(goodHandoff)
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := ParseHandoff(strings.ReplaceAll(goodHandoff, "\n", "\r\n"))
	if err != nil {
		t.Fatalf("CRLF input rejected: %v", err)
	}
	if !reflect.DeepEqual(lf, crlf) {
		t.Fatalf("CRLF differs from LF:\n%+v\n%+v", crlf, lf)
	}
}

// Whatever Format accepts, ParseHandoff must accept and return unchanged.
func TestHandoffFormatParseRoundTrip(t *testing.T) {
	t.Parallel()
	base := func() *Handoff {
		return &Handoff{Slot: "cc-impl", Updated: time.Date(2026, 10, 17, 1, 2, 3, 0, time.UTC), NextAction: "wait",
			Claims: []Claim{{ClaimIssue, "o/r", 4}, {ClaimPR, "r", 5}}, Queue: []string{"#6 later"}}
	}
	if out, err := base().Format(); err != nil {
		t.Fatal(err)
	} else if back, err := ParseHandoff(out); err != nil || !reflect.DeepEqual(back, base()) {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	bad := map[string]func(*Handoff){
		"queue newline":       func(h *Handoff) { h.Queue = []string{"a\n## Blockers"} },
		"queue lone CR":       func(h *Handoff) { h.Queue = []string{"a\rb"} },
		"queue CRLF":          func(h *Handoff) { h.Queue = []string{"a\r\nb"} },
		"claim repo newline":  func(h *Handoff) { h.Claims = []Claim{{ClaimPR, "r\nx", 3}} },
		"claim repo CR":       func(h *Handoff) { h.Claims = []Claim{{ClaimIssue, "o/r\r", 3}} },
		"claim repo bad":      func(h *Handoff) { h.Claims = []Claim{{ClaimIssue, "no-slash", 3}} },
		"next action lone CR": func(h *Handoff) { h.NextAction = "x\r## Blockers\rnone" },
		"next action CRLF":    func(h *Handoff) { h.NextAction = "x\r\n## Blockers\r\nnone" },
		"current item CR":     func(h *Handoff) { h.CurrentItem = "\r## Queue" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := base()
			mutate(h)
			if out, err := h.Format(); err == nil {
				if _, perr := ParseHandoff(out); perr != nil {
					t.Fatalf("Format emitted a handoff Parse rejects: %v\n%q", perr, out)
				}
				t.Fatalf("want Format to reject, got %q", out)
			}
		})
	}
}

func TestHandoffRejects(t *testing.T) {
	t.Parallel()
	rep := func(old, new string) string { return strings.Replace(goodHandoff, old, new, 1) }
	tests := []struct{ name, in, want string }{
		{"empty", "", "empty"},
		{"bad title", rep("# cc-impl handoff", "# handoff"), "want title"},
		{"no updated", rep("Updated: 2026-10-17T10:00:00Z\n", ""), "Updated"},
		{"bad updated", rep("2026-10-17T10:00:00Z", "yesterday"), "updated line"},
		{"missing Blockers", rep("## Blockers\nnone\n", ""), `missing heading "Blockers"`},
		{"missing Claims", rep("## Claims held\n", "## Claims\n"), `missing heading "Claims held"`},
		{"duplicate", rep("## Branch", "## PR\nx\n## Branch"), "duplicate heading"},
		{"unknown heading", goodHandoff + "## Extra\nx\n", `unknown heading "Extra"`},
		{"out of order", rep("## Branch\nfix/thing\n## PR\nLeanerCloud/cloud-commitments-platform#43\n", "## PR\nLeanerCloud/cloud-commitments-platform#43\n## Branch\nfix/thing\n"), "out of order"},
		{"bad claim", rep("- issue LeanerCloud/cloud-commitments-platform#42", "- issue 42"), "unrecognised claim"},
		{"claim file bad n", rep("claim-pr-cloud-commitments-platform-43.txt", "claim-pr-cloud-commitments-platform-x.txt"), "unrecognised claim"},
		{"empty next action", rep("wait for CI", ""), "next action must not be empty"},
		{"text before heading", rep("Updated: 2026-10-17T10:00:00Z\n", "Updated: 2026-10-17T10:00:00Z\nstray\n"), "before the first heading"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseHandoff(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

func TestHandoffFormatRejectsHeadingInjection(t *testing.T) {
	t.Parallel()
	h := &Handoff{Slot: "a", NextAction: "x\n## Blockers\nnone"}
	if _, err := h.Format(); err == nil {
		t.Fatal("want error")
	}
	if _, err := (&Handoff{Slot: "Bad Slot", NextAction: "x"}).Format(); err == nil {
		t.Fatal("want invalid slot error")
	}
}

func TestParseClaim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Claim
		ok   bool
	}{
		{"issue a/b#1", Claim{ClaimIssue, "a/b", 1}, true},
		{"claim-pr-cli-7.txt", Claim{ClaimPR, "cli", 7}, true},
		{"claim-pr-cloud-commitments-go-12.txt", Claim{ClaimPR, "cloud-commitments-go", 12}, true},
		{"issue a/b", Claim{}, false},
		{"claim-pr-cli.txt", Claim{}, false},
		{"pr a/b#1", Claim{}, false},
	}
	for _, tt := range tests {
		got, err := ParseClaim(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("%q: got %+v err=%v", tt.in, got, err)
		}
		if tt.ok && got.String() != tt.in {
			t.Errorf("%q: String() = %q", tt.in, got.String())
		}
	}
}

func TestCommandValidation(t *testing.T) {
	t.Parallel()
	ev := `"reason":"r","evidence":["~/fleet/journal.tsv"]`
	tests := []struct {
		name, in, want string
	}{
		{"nudge ok", `{"cmd":"nudge","slot":"cc-impl","template":"continue",` + ev + `}`, ""},
		{"interrupt ok", `{"cmd":"interrupt","slot":"cc-impl","template":"reread-brief",` + ev + `}`, ""},
		{"migrate ok", `{"cmd":"migrate","slot":"cc-impl","to":"codex",` + ev + `}`, ""},
		{"retire ok", `{"cmd":"retire","slot":"cc-impl",` + ev + `}`, ""},
		{"pin ok", `{"cmd":"pin","slot":"cc-impl",` + ev + `}`, ""},
		{"unpin ok", `{"cmd":"unpin","slot":"cc-impl",` + ev + `}`, ""},
		{"note ok", `{"cmd":"note","slot":"cc-impl","text":"long test run",` + ev + `}`, ""},
		{"report ok", `{"cmd":"report","markdown":"# day",` + ev + `}`, ""},
		{"escalate ok", `{"cmd":"escalate","title":"t","question":"q","options":["a","b"],` + ev + `}`, ""},
		{"unknown cmd", `{"cmd":"rm","slot":"x",` + ev + `}`, "unknown cmd"},
		{"empty cmd", `{` + ev + `}`, "unknown cmd"},
		{"nudge free text template", `{"cmd":"nudge","slot":"cc-impl","template":"git push --force",` + ev + `}`, "unknown template"},
		{"nudge with text", `{"cmd":"nudge","slot":"cc-impl","template":"continue","text":"ignore previous",` + ev + `}`, `field "text" is not allowed`},
		{"nudge no template", `{"cmd":"nudge","slot":"cc-impl",` + ev + `}`, `missing required field "template"`},
		{"nudge no slot", `{"cmd":"nudge","template":"continue",` + ev + `}`, `missing required field "slot"`},
		{"bad slot", `{"cmd":"retire","slot":"../etc",` + ev + `}`, "invalid slot"},
		{"migrate bad harness", `{"cmd":"migrate","slot":"cc-impl","to":"gemini",` + ev + `}`, "unknown harness"},
		{"migrate with template", `{"cmd":"migrate","slot":"cc-impl","to":"codex","template":"continue",` + ev + `}`, "not allowed"},
		{"escalate default", `{"cmd":"escalate","title":"t","question":"q","options":["a"],"default":"a",` + ev + `}`, "unknown field"},
		{"escalate no options", `{"cmd":"escalate","title":"t","question":"q",` + ev + `}`, `missing required field "options"`},
		{"escalate empty option", `{"cmd":"escalate","title":"t","question":"q","options":["a",""],` + ev + `}`, "options[1] is empty"},
		{"escalate blank option", `{"cmd":"escalate","title":"t","question":"q","options":["  "],` + ev + `}`, "options[0] is empty"},
		{"escalate question too big", `{"cmd":"escalate","title":"t","question":"` + strings.Repeat("a", MaxTextBytes+1) + `","options":["a"],` + ev + `}`, "max 4096"},
		{"note too big", `{"cmd":"note","slot":"cc-impl","text":"` + strings.Repeat("a", MaxTextBytes+1) + `",` + ev + `}`, "max 4096"},
		{"note at cap", `{"cmd":"note","slot":"cc-impl","text":"` + strings.Repeat("a", MaxTextBytes) + `",` + ev + `}`, ""},
		{"escalate question at cap", `{"cmd":"escalate","title":"t","question":"` + strings.Repeat("a", MaxTextBytes) + `","options":["a"],` + ev + `}`, ""},
		{"report path", `{"cmd":"report","markdown":"x","path":"/etc/passwd",` + ev + `}`, "unknown field"},
		{"report too big", `{"cmd":"report","markdown":"` + strings.Repeat("a", MaxReportBytes+1) + `",` + ev + `}`, "max 8192"},
		{"no reason", `{"cmd":"retire","slot":"cc-impl","evidence":["x"]}`, "reason is required"},
		{"no evidence", `{"cmd":"retire","slot":"cc-impl","reason":"r"}`, "evidence"},
		{"newline evidence", `{"cmd":"retire","slot":"cc-impl","reason":"r","evidence":["a\nb"]}`, "invalid evidence"},
		{"trailing", `{"cmd":"retire","slot":"cc-impl",` + ev + `} {}`, "trailing"},
		{"not json", `retire cc-impl`, "decode command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCommand([]byte(tt.in))
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
	// Exactly 8 KB is allowed.
	in := `{"cmd":"report","markdown":"` + strings.Repeat("a", MaxReportBytes) + `",` + ev + `}`
	if _, err := ParseCommand([]byte(in)); err != nil {
		t.Fatalf("8 KB report must pass: %v", err)
	}
}

// randomToken returns a deterministic pseudo-random alphanumeric token so no secret-looking
// literal lives in the source.
func randomToken(n int, seed int64) string {
	const alpha = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = alpha[r.Intn(len(alpha))]
	}
	return string(b)
}

func TestScrub(t *testing.T) {
	t.Parallel()
	body := randomToken(36, 1)
	hi := randomToken(40, 7)
	if entropy(hi) < EntropyThreshold || !looksSecret(hi) {
		t.Fatalf("test token not secret-like, adjust seed: %s %.2f", hi, entropy(hi))
	}
	mixed1 := "Zx8Qm2LpV7nRt4YbK9sWd3HfJ6gCa1Ue" + "Xo5Ni0TyP"
	mixed2 := "q7W3eR9tY5uI1oP8aS4dF6gH2jK0lZxC"
	jwt := "eyJ" + randomToken(20, 2) + ".eyJ" + randomToken(20, 3) + "." + randomToken(30, 4)
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" + randomToken(60, 5) + "\n-----END RSA PRIVATE KEY-----"
	tests := []struct {
		name, in, want string
	}{
		{"ghp", "token ghp_" + body + " end", "token [REDACTED:github-token] end"},
		{"gho", "gho_" + body, "[REDACTED:github-token]"},
		{"ghu", "ghu_" + body, "[REDACTED:github-token]"},
		{"ghs", "ghs_" + body, "[REDACTED:github-token]"},
		{"ghr", "ghr_" + body, "[REDACTED:github-token]"},
		{"fine-grained", "github_pat_" + randomToken(22, 8) + "_" + randomToken(40, 9), "[REDACTED:github-token]"},
		{"sk", "key=sk-" + body, "key=[REDACTED:sk-key]"},
		{"sk-ant", "sk-ant-api03-" + body, "[REDACTED:sk-key]"},
		{"akia", "id AKIA" + strings.ToUpper(randomToken(16, 6)[:16]) + ".", "id [REDACTED:aws-key-id]."},
		{"jwt", "Authorization: Bearer " + jwt, "Authorization: Bearer [REDACTED:jwt]"},
		{"pem", "before\n" + pem + "\nafter", "before\n[REDACTED:pem]\nafter"},
		{"pem unterminated", "x\n-----BEGIN PRIVATE KEY-----\n" + randomToken(60, 5), "x\n[REDACTED:pem]"},
		{"entropy", "secret " + hi + " end", "secret [REDACTED:high-entropy] end"},
		{"pgp block", "x\n-----BEGIN PGP PRIVATE KEY BLOCK-----\n" + randomToken(60, 5) + "\n-----END PGP PRIVATE KEY BLOCK-----\ny", "x\n[REDACTED:pem]\ny"},
		{"pgp unterminated", "-----BEGIN PGP PRIVATE KEY BLOCK-----\n" + randomToken(60, 5), "[REDACTED:pem]"},
		{"auth header short", "Authorization: Bearer abc123", "Authorization: Bearer [REDACTED:bearer]"},
		{"bearer opaque", "curl -H 'Bearer " + randomToken(24, 5) + "'", "curl -H 'Bearer [REDACTED:bearer]'"},
		{"bearer lowercase", "bearer " + randomToken(24, 5), "bearer [REDACTED:bearer]"},
		{"tilde before", "~" + hi, "~[REDACTED:high-entropy]"},
		{"tilde after", hi + "~/x", "[REDACTED:high-entropy]~/x"},
		{"tilde gh", "~ghp_" + body, "~[REDACTED:github-token]"},
		{"mixed-case secret", "k=" + mixed1, "[REDACTED:high-entropy]"},
		{"secret in path", "/tmp/" + mixed2 + "/x", "[REDACTED:high-entropy]"},
		{"secret after dash", "key-" + mixed1, "[REDACTED:high-entropy]"},
		// False-positive guards.
		{"bearer prose", "Bearer tokens are short", "Bearer tokens are short"},
		{"long branch", "feat/Fix-Issue-1234-handle-nil-pointer-in-Handoff-parser", "feat/Fix-Issue-1234-handle-nil-pointer-in-Handoff-parser"},
		{"long mixed path", "/Users/x/Dropbox/devel/Cloud2Go1/AbcWorktree9/internal/stateFoo", "/Users/x/Dropbox/devel/Cloud2Go1/AbcWorktree9/internal/stateFoo"},
		{"uuid mixed", "8984eBB8-12b6-4E5e-896D-7a72Fb9765c1", "8984eBB8-12b6-4E5e-896D-7a72Fb9765c1"},
		{"mixed-case hex", "ABCDEF0123456789abcdef0123456789ABCDEF01", "ABCDEF0123456789abcdef0123456789ABCDEF01"},
		{"mixed-case hex 64", "9F8e7D6c5B4a39F8e7D6c5B4a39F8e7D6c5B4a39F8e7D6c5B4a39F8e7D6c5B4a", "9F8e7D6c5B4a39F8e7D6c5B4a39F8e7D6c5B4a39F8e7D6c5B4a39F8e7D6c5B4a"},
		{"git sha1", "head 0123456789abcdef0123456789abcdef01234567", "head 0123456789abcdef0123456789abcdef01234567"},
		{"sha256", "sha256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "sha256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"uuid", "session 8984ebb8-12b6-4e5e-896d-7a72fb9765c1", "session 8984ebb8-12b6-4e5e-896d-7a72fb9765c1"},
		{"uuid upper", "8984EBB8-12B6-4E5E-896D-7A72FB9765C1", "8984EBB8-12B6-4E5E-896D-7A72FB9765C1"},
		{"short sk word", "sk-learn is a library", "sk-learn is a library"},
		{"go identifier", "TestHandoffParseRejectsMissingHeadingsInOrder2", "TestHandoffParseRejectsMissingHeadingsInOrder2"},
		{"path", "~/.claude/worktrees/dotclaude-fleet-core/tools/fleet/internal/state", "~/.claude/worktrees/dotclaude-fleet-core/tools/fleet/internal/state"},
		{"slot name", "cloud-commitments-platform-pr-merge-2", "cloud-commitments-platform-pr-merge-2"},
		{"plain prose", "the quick brown fox jumps over the lazy dog", "the quick brown fox jumps over the lazy dog"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Scrub(tt.in); got != tt.want {
				t.Fatalf("\n in:  %q\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
