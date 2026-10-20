package fleetd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

func (o Observer) Tick(ctx context.Context, paths ObserverPaths, now time.Time) (result Evaluation, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	defer func() {
		if err != nil {
			result.Complete = false
			result.Proposals = nil
			result.Errors = append(result.Errors, state.Scrub(err.Error()))
		}
	}()
	cfg, err := config.Parse(paths.Config.Bytes())
	if err != nil {
		return result, err
	}
	ledger, err := state.ParseLedger(paths.Ledger.Bytes())
	if err != nil {
		return result, err
	}
	out, err := openObserverOutput(ctx, paths)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, out.close()) }()
	memory, err := out.loadMemory()
	if err != nil {
		return result, err
	}
	observation := o.Collect(ctx, paths.Config.Path(), *cfg, *ledger, now)
	result, next := Evaluate(*cfg, now, observation, memory)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !result.Complete {
		err = fmt.Errorf("observer collection incomplete")
	}
	err = errors.Join(err, out.persist(observerRecord{Time: now, Observation: observation, Evaluation: result}, next))
	return result, err
}
