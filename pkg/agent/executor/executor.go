package executor

import (
	"context"
	"sync"
	"time"
)

type Executor struct {
	concurrency int
	sem         chan struct{}
}

func New(concurrency int) *Executor {
	if concurrency <= 0 {
		concurrency = 4
	}
	return &Executor{
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}
}

type Task struct {
	ID      string
	Fn      func(ctx context.Context) (string, error)
	Timeout time.Duration
	OnStart func()
	OnDone  func(Result)
}

type Result struct {
	ID      string
	Output  string
	Err     error
	Elapsed time.Duration
}

type BatchResult struct {
	Results []Result
}

func (e *Executor) RunBatch(ctx context.Context, tasks []Task) BatchResult {
	br := BatchResult{Results: make([]Result, len(tasks))}
	if len(tasks) == 0 {
		return br
	}

	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t Task) {
			defer wg.Done()

			select {
			case e.sem <- struct{}{}:
			case <-ctx.Done():
				br.Results[idx] = Result{ID: t.ID, Err: ctx.Err()}
				return
			}
			defer func() { <-e.sem }()

			if t.OnStart != nil {
				t.OnStart()
			}

			taskCtx := ctx
			if t.Timeout > 0 {
				var cancel context.CancelFunc
				taskCtx, cancel = context.WithTimeout(ctx, t.Timeout)
				defer cancel()
			}

			start := time.Now()
			output, err := t.Fn(taskCtx)
			elapsed := time.Since(start)

			result := Result{
				ID:      t.ID,
				Output:  output,
				Err:     err,
				Elapsed: elapsed,
			}
			br.Results[idx] = result

			if t.OnDone != nil {
				t.OnDone(result)
			}
		}(i, task)
	}
	wg.Wait()

	return br
}
