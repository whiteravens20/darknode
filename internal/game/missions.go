// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// Work comes as contracts. The board holds a few at a time, drawn from the
// catalogue for this run alone: the target is always a server the player can
// reach, and what counts as done is read from what they actually did to it,
// never from what the browser says.
const (
	// MaxAvailable is how many contracts stand on the board.
	MaxAvailable = 6
	// MaxAccepted is how many a player may hold at once.
	MaxAccepted = 5
	// GameHour is an hour of the game's clock, in seconds of online time.
	GameHour = 60
)

// The kinds of contract this milestone carries.
const (
	MissionSteal   = "STEAL_FILE"
	MissionDestroy = "DESTROY_FILE"
)

// difficulty is a band of contract: what it pays, what it asks of the
// player's name, and how long they have. The security band is what makes the
// reputation gate mean something — a harder contract is on a harder server.
type difficulty struct {
	name                 string
	minReward, maxReward int64
	reputationRequired   int
	reputationGain       int
	// deadlineHours is on the game's clock, counted from acceptance; zero
	// means there is no deadline.
	deadlineHours int
	// minSecurity and maxSecurity are the servers this band draws from.
	minSecurity, maxSecurity int
}

var difficulties = []difficulty{
	{"TRIVIAL", 300, 1000, 0, 1, 0, 1, 2},
	{"EASY", 1000, 3000, 3, 2, 72, 3, 4},
	{"MEDIUM", 3000, 8000, 8, 4, 48, 5, 6},
	{"HARD", 8000, 15000, 15, 6, 36, 7, 8},
	{"INSANE", 15000, 30000, 25, 8, 24, 9, 10},
}

// fillBoard draws contracts until the board is full. It is deterministic for
// a run: the same run always draws the same work, so nothing about a contract
// depends on when a player happens to look.
func (e *Env) fillBoard(ctx context.Context, run *store.Run) ([]store.Mission, error) {
	missions, err := e.Store.MissionsOfRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	standing := 0
	taken := map[string]bool{} // a target already under contract is left alone
	for _, m := range missions {
		if m.State == store.MissionAvailable {
			standing++
		}
		if m.State == store.MissionAvailable || m.State == store.MissionAccepted {
			taken[m.Target] = true
		}
	}
	// Nothing is offered on a file this run has already destroyed: it is not
	// there to take or to destroy again.
	gone, err := e.Store.GoneFiles(ctx, run.ID)
	if err != nil {
		return nil, err
	}

	var fresh []store.Mission
	for i := len(missions); standing < MaxAvailable; i++ {
		// One stream of numbers per run, per draw: the same run drawing its
		// fourth contract always draws the same one.
		dice := rand.New(rand.NewPCG(uint64(run.ID), uint64(i)))
		m, ok := e.drawMission(run, dice, taken, gone, false)
		if !ok {
			break // the catalogue has nothing left this player could take on
		}
		taken[m.Target] = true
		fresh = append(fresh, m)
		standing++
	}

	// A board a player cannot take anything from is not a board. The draw
	// reaches a band above their name on purpose, and a run of those can fill
	// the board with work that is only something to aim at; when that
	// happens, one contract is drawn that they can actually take.
	if !withinReach(missions, fresh, run.Reputation) {
		dice := rand.New(rand.NewPCG(uint64(run.ID), uint64(len(missions)+len(fresh)+1)))
		if m, ok := e.drawMission(run, dice, taken, gone, true); ok {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) == 0 {
		return missions, nil
	}
	if err := e.Store.AddMissions(ctx, fresh); err != nil {
		return nil, err
	}
	return e.Store.MissionsOfRun(ctx, run.ID)
}

// withinReach reports whether anything on the board can be taken on by a
// player of this reputation.
func withinReach(standing, fresh []store.Mission, reputation int) bool {
	for _, group := range [][]store.Mission{standing, fresh} {
		for _, m := range group {
			if m.State == store.MissionAvailable && m.ReputationRequired <= reputation {
				return true
			}
		}
	}
	return false
}

// drawMission makes one contract for the run. With reach set, it stays inside
// what the player's name can take instead of reaching a band above it.
func (e *Env) drawMission(run *store.Run, dice *rand.Rand, taken, gone map[string]bool, reach bool) (store.Mission, bool) {
	// A band the player's name can carry, with a chance of one above it: a
	// board of nothing but easy work is no board at all.
	var bands []difficulty
	for _, d := range difficulties {
		if d.reputationRequired <= run.Reputation {
			bands = append(bands, d)
		}
	}
	if len(bands) == 0 {
		bands = difficulties[:1]
	}
	band := bands[dice.IntN(len(bands))]
	if !reach && len(bands) < len(difficulties) && dice.IntN(4) == 0 {
		band = difficulties[len(bands)]
	}

	// A target in that band with something worth taking on it.
	type candidate struct {
		server *world.Server
		file   world.File
	}
	var pool []candidate
	for _, s := range e.World.Servers {
		if s.Type().Public() || taken[s.ID] ||
			s.Security < band.minSecurity || s.Security > band.maxSecurity {
			continue
		}
		for _, f := range s.Files {
			if f.Sensitive && !f.Encrypted && !gone[s.ID+"\x00"+f.Path] {
				pool = append(pool, candidate{server: s, file: f})
			}
		}
	}
	if len(pool) == 0 {
		return store.Mission{}, false
	}
	pick := pool[dice.IntN(len(pool))]

	kind := MissionSteal
	if dice.IntN(2) == 0 {
		kind = MissionDestroy
	}
	reward := band.minReward + int64(dice.IntN(int(band.maxReward-band.minReward+1)))
	return store.Mission{
		RunID: run.ID, Kind: kind, Target: pick.server.ID, File: pick.file.Path,
		Client: e.client(dice, pick.server), Difficulty: band.name, Reward: reward,
		ReputationRequired: band.reputationRequired, ReputationGain: band.reputationGain,
		Deadline: int64(band.deadlineHours) * GameHour,
		State:    store.MissionAvailable, CreatedAt: e.Now(),
	}, true
}

// client is who is paying: another organisation of the catalogue, or nobody
// who gave a name.
func (e *Env) client(dice *rand.Rand, target *world.Server) string {
	var others []string
	for _, org := range e.World.Orgs {
		if org.ID != target.Org.ID && !org.Type.Public() {
			others = append(others, org.Name)
		}
	}
	if len(others) == 0 {
		return "a client who did not give a name"
	}
	return others[dice.IntN(len(others))]
}

func (e *Env) missions(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) > 0 {
		return e.missionCmd(ctx, c)
	}
	missions, err := e.fillBoard(ctx, c.run)
	if err != nil {
		return Result{}, err
	}

	board := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-3s %-34s %-24s %9s %s", "#", "WORK", "TARGET", "PAYS", "NEEDS"),
		Kind: KindHead,
	}}}
	var yours []Line
	for i, m := range missions {
		number := i + 1
		switch m.State {
		case store.MissionAvailable:
			board.Lines = append(board.Lines, Line{
				Text: fmt.Sprintf("  %-3d %-34s %-24s %9s rep %d", number,
					e.missionWork(m), e.serverName(m.Target), "$"+money(m.Reward), m.ReputationRequired),
			})
		case store.MissionAccepted:
			yours = append(yours, Line{
				Text: fmt.Sprintf("  %-3d %-34s %-24s %9s %s", number,
					e.missionWork(m), e.serverName(m.Target), "$"+money(m.Reward), e.missionLeft(m, c.run)),
			})
		}
	}
	if len(board.Lines) == 1 {
		board.Lines = append(board.Lines, Line{Text: "  Nothing on the board.", Kind: KindDim})
	}
	if len(yours) > 0 {
		board.Lines = append(board.Lines, Line{Text: "YOURS", Kind: KindHead})
		board.Lines = append(board.Lines, yours...)
	}
	board.Lines = append(board.Lines,
		Line{Text: fmt.Sprintf("  rep %d. `mission accept <#>`, `mission submit <#>`, `mission abandon <#>`.",
			c.run.Reputation), Kind: KindDim})
	return board, nil
}

// missionWork says what a contract asks for, in the player's words.
func (e *Env) missionWork(m store.Mission) string {
	if m.Kind == MissionDestroy {
		return "destroy " + m.File
	}
	return "steal " + m.File
}

// missionLeft says how much online time a contract has left.
func (e *Env) missionLeft(m store.Mission, run *store.Run) string {
	if m.Deadline == 0 || m.AcceptedAtExposure == nil {
		return "no deadline"
	}
	left := *m.AcceptedAtExposure + m.Deadline - run.ExposureSeconds
	if left <= 0 {
		return "out of time"
	}
	return fmt.Sprintf("%s of play left", FormatGameDuration(left))
}

// serverName names a server for the player, falling back to its id.
func (e *Env) serverName(id string) string {
	if s, ok := e.World.ServerByID(id); ok {
		return s.Name
	}
	return id
}

func (e *Env) missionCmd(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) != 2 {
		return errorResult("Usage: mission accept <#>, mission submit <#>, mission abandon <#>"), nil
	}
	number, err := strconv.Atoi(c.args[1])
	if err != nil || number < 1 {
		return errorResult("Usage: mission accept <#>"), nil
	}
	missions, err := e.Store.MissionsOfRun(ctx, c.run.ID)
	if err != nil {
		return Result{}, err
	}
	if number > len(missions) {
		return errorResult(fmt.Sprintf("There is no contract %d. `missions` lists them.", number)), nil
	}
	m := missions[number-1]

	switch strings.ToLower(c.args[0]) {
	case "accept":
		return e.missionAccept(ctx, c, m, missions)
	case "submit":
		return e.missionSubmit(ctx, c, m)
	case "abandon":
		return e.missionAbandon(ctx, c, m)
	default:
		return errorResult("Usage: mission accept <#>, mission submit <#>, mission abandon <#>"), nil
	}
}

func (e *Env) missionAccept(ctx context.Context, c cmd, m store.Mission, all []store.Mission) (Result, error) {
	if m.State != store.MissionAvailable {
		return errorResult("That contract is not on the board."), nil
	}
	if c.run.Reputation < m.ReputationRequired {
		return errorResult(fmt.Sprintf("That contract wants a reputation of %d; yours is %d.",
			m.ReputationRequired, c.run.Reputation)), nil
	}
	held := 0
	for _, other := range all {
		if other.State == store.MissionAccepted {
			held++
		}
	}
	if held >= MaxAccepted {
		return errorResult(fmt.Sprintf("You are holding %d contracts already.", held)), nil
	}

	exposure, at := c.run.ExposureSeconds, e.Now()
	if err := e.Store.SetMissionState(ctx, m.ID, store.MissionAccepted, &exposure, &at); err != nil {
		return Result{}, err
	}
	// The contract comes with the address: a client who wants something from
	// a machine says which machine. This is one of the two ways the list of
	// addresses grows.
	if err := e.Store.LearnServer(ctx, c.run.ID, m.Target); err != nil {
		return Result{}, err
	}
	address := ""
	if s, ok := e.World.ServerByID(m.Target); ok {
		address = " at " + s.IP.String()
	}
	lines := []Line{{
		Text: fmt.Sprintf("Taken on: %s on %s%s, for %s. $%s on delivery.",
			e.missionWork(m), e.serverName(m.Target), address, m.Client, money(m.Reward)),
		Kind: KindOK,
	}}
	if m.Deadline > 0 {
		lines = append(lines, Line{
			Text: fmt.Sprintf("  %s of play to do it in.", FormatGameDuration(m.Deadline)),
			Kind: KindDim,
		})
	}
	return Result{Changed: true, Lines: lines}, nil
}

func (e *Env) missionSubmit(ctx context.Context, c cmd, m store.Mission) (Result, error) {
	if m.State != store.MissionAccepted {
		return errorResult("That contract is not one of yours."), nil
	}
	// Out of time is out of time, whatever was done.
	if m.Deadline > 0 && m.AcceptedAtExposure != nil &&
		c.run.ExposureSeconds > *m.AcceptedAtExposure+m.Deadline {
		if err := e.Store.SetMissionState(ctx, m.ID, store.MissionFailed, m.AcceptedAtExposure, m.AcceptedAt); err != nil {
			return Result{}, err
		}
		return Result{Changed: true, Lines: []Line{
			{Text: "You are past the deadline. The contract is closed.", Kind: KindError},
		}}, nil
	}

	done, why, err := e.missionDone(ctx, c.run, m)
	if err != nil {
		return Result{}, err
	}
	if !done {
		return errorResult(why), nil
	}
	if err := e.Store.Credit(ctx, c.run.ID, m.Reward, 0); err != nil {
		return Result{}, err
	}
	if err := e.Store.AddReputation(ctx, c.run.ID, m.ReputationGain); err != nil {
		return Result{}, err
	}
	if err := e.Store.SetMissionState(ctx, m.ID, store.MissionDone, m.AcceptedAtExposure, m.AcceptedAt); err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Lines: []Line{
		{Text: fmt.Sprintf("Delivered. $%s paid, and %s thinks better of you by %d.",
			money(m.Reward), m.Client, m.ReputationGain), Kind: KindOK},
	}}, nil
}

// missionDone reads the state of the run to decide whether a contract is
// done. Nothing the browser sends is believed: what counts is the copy on the
// gateway, or the file that is gone — and either must have happened after the
// contract was taken on.
func (e *Env) missionDone(ctx context.Context, run *store.Run, m store.Mission) (bool, string, error) {
	switch m.Kind {
	case MissionSteal:
		files, err := e.Store.GatewayFiles(ctx, run.ID)
		if err != nil {
			return false, "", err
		}
		for _, f := range files {
			if f.Server != m.Target || f.Path != m.File {
				continue
			}
			if m.AcceptedAt != nil && f.CopiedAt.Before(*m.AcceptedAt) {
				return false, "You had that copy before you took the contract. Take it again.", nil
			}
			return true, "", nil
		}
		return false, fmt.Sprintf("You do not have %s. `cp %s` on %s, with root.",
			m.File, m.File, e.serverName(m.Target)), nil

	case MissionDestroy:
		gone, err := e.Store.DeletedFiles(ctx, run.ID, m.Target)
		if err != nil {
			return false, "", err
		}
		at, deleted := gone[m.File]
		if !deleted {
			return false, fmt.Sprintf("%s is still there. `rm %s` on %s, with root.",
				m.File, m.File, e.serverName(m.Target)), nil
		}
		if m.AcceptedAt != nil && at.Before(*m.AcceptedAt) {
			return false, "That was already gone before you took the contract.", nil
		}
		return true, "", nil
	}
	return false, "Nobody knows what that contract wanted.", nil
}

func (e *Env) missionAbandon(ctx context.Context, c cmd, m store.Mission) (Result, error) {
	if m.State != store.MissionAccepted {
		return errorResult("That contract is not one of yours."), nil
	}
	if err := e.Store.SetMissionState(ctx, m.ID, store.MissionFailed, m.AcceptedAtExposure, m.AcceptedAt); err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Lines: []Line{
		{Text: "Dropped. Whoever was paying will remember it.", Kind: KindOK},
	}}, nil
}
