// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// The programs a file needs. Reading is free once there is root; taking a
// copy of something somebody marked sensitive, or destroying it, is not.
const (
	fileCopier   = "file_copier"
	fileShredder = "file_shredder"
)

// transfer is a copy on its way to the gateway. It is the one thing the modem
// decides, and the trace runs while it happens.
type transfer struct {
	path   string
	sizeMB int
	// left is how much of the transfer is still to come.
	left time.Duration
}

// TransferTime is how long a file takes to come down the line: its size
// against the gateway's modem, and nothing else.
func TransferTime(sizeMB, modemMbps int) time.Duration {
	if modemMbps <= 0 {
		return 0
	}
	seconds := float64(sizeMB) * 8 / float64(modemMbps)
	return time.Duration(seconds * float64(time.Second))
}

// StartTransfer begins taking a copy of a file on the connected server. Only
// one copy comes down the line at a time.
func (e *Engine) StartTransfer(account int64, path string, sizeMB, modemMbps int) (time.Duration, error) {
	e.mu.Lock()
	r := e.live[account]
	if r == nil || r.conn == nil {
		e.mu.Unlock()
		return 0, ErrNoConnection
	}
	if r.conn.transfer != nil {
		e.mu.Unlock()
		return 0, ErrTransferRunning
	}
	left := TransferTime(sizeMB, modemMbps)
	r.conn.transfer = &transfer{path: path, sizeMB: sizeMB, left: left}
	view := r.conn.viewWithCPU(r.conn.cpuMHz)
	e.mu.Unlock()

	e.out.Publish(account, DeltaConnection, ConnectionDelta{Connection: view})
	return left, nil
}

// ErrTransferRunning is returned when a copy is already coming down the line.
var ErrTransferRunning = fmt.Errorf("a file is already on its way")

func (e *Env) cp(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: cp <path>"), nil
	}
	if c.conn.State != StateConnected {
		return errorResult("The connection is still coming up."), nil
	}
	file, out := e.fileHere(ctx, c, c.args[0])
	if file == nil {
		return out, nil
	}
	if !e.mayRead(c.conn, *file) {
		return errorResult("Not without root here. Open the password layer first."), nil
	}
	if (file.Sensitive || file.Encrypted) && !hasTool(c.run, fileCopier) {
		return e.missingTool(fileCopier), nil
	}

	live, err := e.live()
	if err != nil {
		return Result{}, err
	}
	left, err := live.StartTransfer(c.p.AccountID, file.Path, file.SizeMB, c.run.Gateway.ModemMbps)
	if err != nil {
		if err == ErrTransferRunning {
			return errorResult("Something is already coming down the line. Wait for it."), nil
		}
		return Result{}, err
	}
	// Taking a copy is an act against the server, so it is one of the things
	// that starts a trace — and the trace runs while the file comes down.
	live.ArmTrace(c.p.AccountID, c.run)

	return Result{Lines: []Line{
		{Text: fmt.Sprintf("%s (%d MB) coming down at %d Mbps — about %.1f s.",
			file.Path, file.SizeMB, c.run.Gateway.ModemMbps, left.Seconds()), Kind: KindOK},
		{Text: "  The trace runs while it does. A faster modem is a shorter exposure.", Kind: KindDim},
	}}, nil
}

func (e *Env) rm(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) != 1 {
		return errorResult("Usage: rm <path>"), nil
	}
	if c.conn.State != StateConnected {
		return errorResult("The connection is still coming up."), nil
	}
	file, out := e.fileHere(ctx, c, c.args[0])
	if file == nil {
		return out, nil
	}
	if !c.conn.Root {
		return errorResult("Not without root here. Open the password layer first."), nil
	}
	if file.Sensitive && !hasTool(c.run, fileShredder) {
		return e.missingTool(fileShredder), nil
	}

	if err := e.Store.DeleteFile(ctx, c.run.ID, c.conn.Target.ID, file.Path, e.Now()); err != nil {
		return Result{}, err
	}
	if err := e.logAct(ctx, c.run.ID, c.conn, ActionDelete, file.Sensitive); err != nil {
		return Result{}, err
	}
	if live, err := e.live(); err == nil {
		live.ArmTrace(c.p.AccountID, c.run)
	}
	return Result{Changed: true, Lines: []Line{
		{Text: fmt.Sprintf("%s is gone.", file.Path), Kind: KindOK},
		{Text: "  Nobody else's copy is touched; this is what you did to this machine.", Kind: KindDim},
	}}, nil
}

// fileHere finds a file on the connected server, as this run sees it: what
// the run has destroyed is not there any more.
func (e *Env) fileHere(ctx context.Context, c cmd, path string) (*world.File, Result) {
	gone, err := e.Store.DeletedFiles(ctx, c.run.ID, c.conn.Target.ID)
	if err != nil {
		return nil, errorResult("Something went wrong.")
	}
	for _, f := range c.conn.Target.Files {
		if f.Path != path {
			continue
		}
		if _, deleted := gone[f.Path]; deleted {
			break
		}
		file := f
		return &file, Result{}
	}
	return nil, errorResult(fmt.Sprintf("No file at %q. `ls` lists what is here.", path))
}

// gatewayFiles is what the run has taken, for `status`.
func (e *Env) gatewayFiles(ctx context.Context, run *store.Run) ([]store.GatewayFile, error) {
	return e.Store.GatewayFiles(ctx, run.ID)
}
