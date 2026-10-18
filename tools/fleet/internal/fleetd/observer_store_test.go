package fleetd

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/limits"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/liveness"
)

func journalFixture(t *testing.T) (*observerOutput, observerRecord, ObserverMemory) {
	t.Helper()
	_, paths := observerOutputFixture(t)
	out, err := openObserverOutput(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.close() })
	id := strings.Repeat("a", 64)
	return out, observerRecord{Observation: Observation{Complete: true, Inventory: []Inventory{{Identity: id}}}}, ObserverMemory{Signatures: map[string]string{}}
}

func TestObserverJournalScrubsAllTextBeforeEncoding(t *testing.T) {
	t.Parallel()
	out, record, mem := journalFixture(t)
	id := record.Observation.Inventory[0].Identity
	secret := "-----BEGIN PRIVATE KEY-----\nprivate material\n-----END PRIVATE KEY-----\ngh" + "p_" + strings.Repeat("x", 24)
	record.Observation.Inventory[0] = Inventory{secret, secret, secret, secret, id, secret}
	record.Observation.Slots = []SlotEvidence{{Name: secret, Identity: id, Harness: secret, Status: secret, Detection: secret, Recent: secret}}
	record.Observation.Errors, record.Observation.Notes = []string{secret}, []string{secret}
	record.Evaluation.Errors, record.Evaluation.Notes = []string{secret}, []string{secret}
	record.Evaluation.Proposals = []Proposal{{Identity: id, Slot: secret, Kind: liveness.Nudge, Reason: secret}}
	record.Evaluation.Limits = map[string]limits.Result{id: {Harness: config.Claude, Signature: secret, ResetText: secret, Reason: secret}}
	before, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := out.persist(record, mem); err != nil {
		t.Fatal(err)
	}
	data, err := out.root.ReadFile(observerJournal)
	if err != nil || bytes.Count(data, []byte("\n")) != 1 || bytes.Contains(data, []byte("private material")) || bytes.Contains(data, []byte(strings.Repeat("x", 24))) {
		t.Fatal("journal did not preserve one scrubbed JSON line", string(data), err)
	}
	var got observerRecord
	if err := json.Unmarshal(data, &got); err != nil || got.Observation.Inventory[0].Identity != id || got.Evaluation.Limits[id].Harness != config.Claude {
		t.Fatal("structural identity lost", err)
	}
	if bytes.Count(data, []byte("[REDACTED:pem]")) != 19 || bytes.Count(data, []byte("[REDACTED:github-token]")) != 19 {
		t.Fatal("one or more text fields were not scrubbed", string(data))
	}
	after, err := json.Marshal(record)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("persistence mutated caller record", err)
	}
	if err := out.persist(record, mem); err != nil {
		t.Fatal(err)
	}
	after, err = out.root.ReadFile(observerJournal)
	if err != nil || !bytes.Equal(after, append(bytes.Clone(data), data...)) {
		t.Fatal("second append changed prior record", err)
	}
	if got, err := out.loadMemory(); err != nil || !reflect.DeepEqual(got, mem) {
		t.Fatal("memory was not persisted", got, err)
	}
}

func TestObserverStoreValidatesBothBeforeWrites(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"memory", "record", "proposal", "limit", "encode"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			out, record, mem := journalFixture(t)
			switch kind {
			case "memory":
				mem.Signatures = nil
			case "record":
				record.Observation.Inventory[0].Identity = "invalid"
			case "proposal":
				record.Evaluation.Proposals = []Proposal{{Identity: strings.Repeat("a", 64), Kind: "invalid"}}
			case "limit":
				record.Evaluation.Limits = map[string]limits.Result{"invalid": {Harness: config.Claude}}
			case "encode":
				record.Evaluation.Resources.Load1 = math.NaN()
			}
			if err := out.persist(record, mem); err == nil {
				t.Fatal("invalid persistence accepted")
			}
			for _, name := range []string{observerJournal, observerMemory} {
				if _, err := out.root.Lstat(name); !os.IsNotExist(err) {
					t.Fatal("validation wrote artifact", name, err)
				}
			}
		})
	}
}

func TestObserverJournalRefusalsPreserveArtifacts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"partial-tail", "symlink", "hardlink", "fifo", "mode", "root", "lock"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			out, record, mem := journalFixture(t)
			prior := []byte("prior\n")
			if kind == "partial-tail" {
				prior = []byte("partial")
			}
			if err := out.root.WriteFile(observerMemory, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = out.root.Symlink(observerMemory, observerJournal)
			case "hardlink":
				err = out.root.Link(observerMemory, observerJournal)
			case "fifo":
				err = syscall.Mkfifo(out.root.Name()+"/"+observerJournal, 0o600)
			case "root":
				err = os.Rename(out.root.Name(), out.root.Name()+"-saved")
			case "lock":
				err = out.lock.Close()
			default:
				mode := os.FileMode(0o600)
				if kind == "mode" {
					mode = 0o644
				}
				err = out.root.WriteFile(observerJournal, prior, mode)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := out.persist(record, mem); err == nil {
				t.Fatal("unsafe persistence accepted")
			} else if kind == "fifo" && !strings.Contains(err.Error(), "regular") {
				t.Fatal("FIFO regular-file guard bypassed", err)
			}
			if data, err := out.root.ReadFile(observerMemory); err != nil || !bytes.Equal(data, prior) {
				t.Fatal("refusal changed prior memory", err)
			}
			if kind == "partial-tail" || kind == "mode" {
				if data, err := out.root.ReadFile(observerJournal); err != nil || !bytes.Equal(data, prior) {
					t.Fatal("refusal changed journal", err)
				}
			}
		})
	}
}

func TestObserverStoreReportsUncertaintyAfterJournal(t *testing.T) {
	t.Parallel()
	out, record, mem := journalFixture(t)
	if err := out.root.Mkdir(observerMemory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := out.persist(record, mem); err == nil || !strings.Contains(err.Error(), "journal appended but memory persistence uncertain") {
		t.Fatal("memory failure not reported as uncertain", err)
	}
	want, err := encodeObserverRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := out.root.ReadFile(observerJournal); err != nil || !bytes.Equal(data, want) {
		t.Fatal("journal rolled back after memory failure", err)
	}
}

func TestObserverJournalPreservesPartialOSWrite(t *testing.T) {
	if os.Getenv("FLEET_JOURNAL_WRITE_LIMIT_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestObserverJournalPreservesPartialOSWrite$")
		cmd.Env = append(os.Environ(), "FLEET_JOURNAL_WRITE_LIMIT_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("limited child: %v %s", err, output)
		}
		return
	}
	out, record, mem := journalFixture(t)
	signal.Ignore(syscall.SIGXFSZ)
	var prior syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &prior); err != nil {
		t.Fatal(err)
	}
	limit := prior
	limit.Cur = 16
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &prior) }()
	if err := out.persist(record, mem); err == nil || !strings.Contains(err.Error(), "journal persistence uncertain") {
		t.Fatal("OS append failure hidden", err)
	}
	data, err := out.root.ReadFile(observerJournal)
	if err != nil || len(data) != 16 || data[len(data)-1] == '\n' {
		t.Fatal("partial OS write was not preserved", len(data), err)
	}
	if _, err := out.root.Stat(observerMemory); !os.IsNotExist(err) {
		t.Fatal("memory written after failed append", err)
	}
	if err := out.persist(record, mem); err == nil || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatal("partial journal reused", err)
	}
}
