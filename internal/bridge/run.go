package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/protocol"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

const Version = "fencelab/bridge-v1"

type Report struct {
	Version         string           `json:"version"`
	Scope           string           `json:"scope"`
	Mode            string           `json:"mode"`
	Seed            string           `json:"seed"`
	Processes       int              `json:"processes"`
	History         []protocol.Entry `json:"history"`
	Replay          model.ReplayFile `json:"replay"`
	Expected        sim.Summary      `json:"expected"`
	Observed        sim.Summary      `json:"observed"`
	MatchesModel    bool             `json:"matches_model"`
	Authority       durable.State    `json:"recovered_authority"`
	Store           durable.State    `json:"recovered_store"`
	NextToken       int              `json:"next_token"`
	RestartVerified bool             `json:"restart_verified"`
}

// Run owns exactly four children and a fresh data directory. The driver is also
// the fault proxy: no independent wall-clock race is inferred from virtual time.
func Run(ctx context.Context, executable, directory string, s model.Scenario, seed int64, replay *model.ReplayFile) (report Report, err error) {
	if err := s.Validate(); err != nil {
		return report, err
	}
	if replay != nil && !reflect.DeepEqual(replay.Scenario, s) {
		return report, fmt.Errorf("replay scenario must match bridge scenario")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return report, fmt.Errorf("create fresh bridge directory: %w", err)
	}
	p := &processes{children: map[string]*child{}, history: []protocol.Entry{}}
	defer func() { err = errors.Join(err, p.close()) }()
	for _, role := range []string{"authority", "worker-a", "worker-b", "store"} {
		wal := ""
		if role == "authority" || role == "store" {
			wal = filepath.Join(directory, role+".wal")
		}
		c, startErr := startChild(ctx, executable, role, wal, s)
		if startErr != nil {
			return report, startErr
		}
		p.children[role] = c
	}
	var result model.Result
	mode := "seeded-delivery"
	if replay != nil {
		mode = "exact-prefix"
		result, err = model.ReplayTransport(ctx, *replay, p)
	} else {
		result, err = model.RunTransport(ctx, s, seed, p)
	}
	if err != nil {
		return report, err
	}
	if result.HaltReason == "event-limit" {
		return report, fmt.Errorf("bridge event limit reached; experiment is incomplete")
	}
	report = Report{Version: Version, Scope: "four real child processes; private pipes; model-controlled virtual delivery; process-crash recovery; not an autonomous cluster",
		Mode: mode, Seed: strconv.FormatInt(seed, 10), Processes: 4, History: p.history, Replay: result.Replay, Expected: result.Summary}
	report.Observed, err = Summarize(report.History)
	if err != nil {
		return report, err
	}
	report.MatchesModel = report.Observed == report.Expected
	if !report.MatchesModel {
		return report, fmt.Errorf("observed history differs from model summary")
	}
	for _, role := range []string{"authority", "store"} {
		if err := p.children[role].stop(true); err != nil {
			return report, err
		}
		path := filepath.Join(directory, role+".wal")
		log, _, err := durable.Open(path, nil)
		if err != nil {
			return report, err
		}
		state := log.Snapshot()
		if err := log.Close(); err != nil {
			return report, err
		}
		if role == "authority" {
			report.Authority = state
		} else {
			report.Store = state
		}
		restarted, err := startChild(ctx, executable, role, path, s)
		if err != nil {
			return report, err
		}
		p.children[role] = restarted
		snapshot, err := restarted.call(protocol.Request{To: role, Operation: "inspect"})
		if err != nil || snapshot != (protocol.Response{Status: "snapshot", Token: state.Epoch, Fence: state.Fence, Effects: len(state.Effects)}) {
			return report, fmt.Errorf("restarted %s differs from recovered WAL: %v", role, err)
		}
	}
	next, err := p.children["authority"].call(protocol.Request{To: "authority", Operation: "reserve", Token: report.Authority.Epoch + 1})
	if err != nil || next.Status != "reserved" || next.Token != report.Authority.Epoch+1 {
		return report, fmt.Errorf("restarted authority reused an epoch: %v", err)
	}
	report.NextToken = next.Token
	report.RestartVerified = true
	if err := Check(ctx, report); err != nil {
		return report, err
	}
	return report, nil
}

// Summarize is an independent history oracle; it counts committed responses,
// not model trace labels or the report's precomputed counters.
func Summarize(history []protocol.Entry) (sim.Summary, error) {
	summary := sim.Summary{Safe: true}
	active, previousTime, count := 0, 0, 0
	keys := map[string]bool{}
	for i, entry := range history {
		r, response := entry.Request, entry.Response
		if entry.Sequence != i+1 || r.AtMS < previousTime {
			return summary, fmt.Errorf("non-monotonic history at entry %d", i)
		}
		previousTime = r.AtMS
		switch r.Operation {
		case "activate":
			if response.Status != "active" || r.Token <= active {
				return summary, fmt.Errorf("invalid activation history")
			}
			active = r.Token
		case "write":
			switch response.Status {
			case "committed":
				count++
				summary.AcceptedWrites++
				if r.Token < active {
					summary.StaleWrites++
				}
				if keys[r.Key] {
					summary.DuplicateWrites++
				}
				keys[r.Key] = true
			case "rejected":
				summary.RejectedWrites++
			case "deduplicated":
				if !keys[r.Key] {
					return summary, fmt.Errorf("deduplication without an earlier effect")
				}
				summary.Deduplicated++
			default:
				return summary, fmt.Errorf("unknown write outcome")
			}
			if response.Effects != count {
				return summary, fmt.Errorf("effect count disagrees with committed history")
			}
		case "result":
			summary.Completed = response.Completed
		case "reserve", "ack", "dispatch", "work", "fence":
		default:
			return summary, fmt.Errorf("unknown history operation")
		}
	}
	summary.Safe = summary.StaleWrites == 0 && summary.DuplicateWrites == 0
	return summary, nil
}

type historyTransport struct {
	history []protocol.Entry
	index   int
}

func (h *historyTransport) Call(_ context.Context, r protocol.Request) (protocol.Response, error) {
	if h.index >= len(h.history) || h.history[h.index].Request != r {
		return protocol.Response{}, fmt.Errorf("history request %d differs from replay", h.index)
	}
	response := h.history[h.index].Response
	h.index++
	return response, nil
}

// Check verifies internal consistency, not the provenance of an imported file.
func Check(ctx context.Context, r Report) error {
	if r.Version != Version || r.Processes != 4 || !r.RestartVerified || len(r.History) > 1024 ||
		(r.Mode != "seeded-delivery" && r.Mode != "exact-prefix") {
		return fmt.Errorf("invalid bridge report header")
	}
	if _, err := strconv.ParseInt(r.Seed, 10, 64); err != nil {
		return fmt.Errorf("invalid bridge seed")
	}
	transport := &historyTransport{history: r.History}
	result, err := model.ReplayTransport(ctx, r.Replay, transport)
	if err != nil {
		return err
	}
	observed, err := Summarize(r.History)
	if err != nil {
		return err
	}
	if transport.index != len(r.History) || result.Summary != r.Expected || observed != r.Observed ||
		observed != result.Summary || !r.MatchesModel {
		return fmt.Errorf("bridge report, model, and observed history disagree")
	}
	authority := durable.State{Effects: []durable.Effect{}}
	store := durable.State{Effects: []durable.Effect{}}
	for _, entry := range r.History {
		if entry.Request.Operation == "reserve" {
			authority.Epoch = entry.Response.Token
			authority.Sequence++
		}
		if entry.Request.Operation == "fence" && entry.Response.Fence > store.Fence {
			store.Fence = entry.Response.Fence
			store.Sequence++
		}
		if entry.Request.Operation == "write" && entry.Response.Status == "committed" {
			store.Sequence++
			store.Effects = append(store.Effects, durable.Effect{Sequence: store.Sequence, Token: entry.Request.Token, Key: entry.Request.Key})
		}
	}
	if !reflect.DeepEqual(r.Authority, authority) || !reflect.DeepEqual(r.Store, store) || r.NextToken != authority.Epoch+1 {
		return fmt.Errorf("recovered durable state disagrees with history")
	}
	return nil
}
