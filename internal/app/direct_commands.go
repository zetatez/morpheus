package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/command"
	"github.com/zetatez/morpheus/internal/session"
)

type DirectCommandHandler func(ctx context.Context, sessionID, args string) (Response, string)

func initDirectCommands(rt *Runtime) map[string]DirectCommandHandler {
	return map[string]DirectCommandHandler{
		"help":    handleHelp(rt),
		"skills":  handleSkills(rt),
		"session": handleSession(rt),
	}
}

func handleHelp(rt *Runtime) DirectCommandHandler {
	return func(ctx context.Context, sessionID, args string) (Response, string) {
		cmds := rt.commands.List()
		sort.Slice(cmds, func(i, j int) bool {
			return cmds[i].Name < cmds[j].Name
		})
		var b strings.Builder
		b.WriteString("**Available commands:**\n\n")
		for _, c := range cmds {
			b.WriteString(fmt.Sprintf("  `/%s`", c.Name))
			if c.Description != "" {
				b.WriteString(" — " + c.Description)
			}
			b.WriteString("\n")
		}

		reply := b.String()
		return Response{Reply: reply}, reply
	}
}

func handleSkills(rt *Runtime) DirectCommandHandler {
	return func(ctx context.Context, sessionID, args string) (Response, string) {
		allSkills := rt.skills.List()
		sort.Slice(allSkills, func(i, j int) bool {
			return allSkills[i].Name < allSkills[j].Name
		})

		var b strings.Builder
		if len(allSkills) == 0 {
			b.WriteString("No skills found.\n")
		} else {
			b.WriteString(fmt.Sprintf("**Available skills (%d):**\n\n", len(allSkills)))
			for _, s := range allSkills {
				b.WriteString(fmt.Sprintf("  `%s`", s.Name))
				if s.Description != "" {
					b.WriteString(" — " + s.Description)
				}
				b.WriteString("\n")
			}
		}

		reply := b.String()
		return Response{Reply: reply}, reply
	}
}

func handleSession(rt *Runtime) DirectCommandHandler {
	return func(ctx context.Context, sessionID, args string) (Response, string) {
		args = strings.TrimSpace(args)

		if args != "" {
			rt.sessionManager.GetOrCreate(args)
			reply := fmt.Sprintf("Switched to session **`%s`**", args)
			return Response{Reply: reply}, reply
		}

		var allSessions []session.Metadata
		if rt.sessionStore != nil {
			sessions, err := rt.sessionStore.ListSessions(ctx, "")
			if err == nil {
				allSessions = sessions
			}
		}

		sort.Slice(allSessions, func(i, j int) bool {
			return allSessions[i].UpdatedAt.After(allSessions[j].UpdatedAt)
		})

		var b strings.Builder
		if len(allSessions) == 0 {
			b.WriteString("No sessions yet.\n")
		} else {
			b.WriteString(fmt.Sprintf("**Sessions (%d):**\n\n", len(allSessions)))
			for _, s := range allSessions {
				label := s.SessionID
				if s.Summary != "" {
					label = s.Summary
				}
				ts := s.UpdatedAt
				if ts.IsZero() {
					ts = time.Now()
				}
				prefix := " "
				if s.SessionID == sessionID {
					prefix = ">"
				}
				b.WriteString(fmt.Sprintf("%s `%s` — %s (%s)\n", prefix, s.SessionID, label, ts.Format("Jan 02 15:04")))
			}
		}

		b.WriteString("\nUsage: `/session <id>` to switch\n")

		reply := b.String()
		return Response{Reply: reply}, reply
	}
}

func checkDirectCommand(rt *Runtime, ctx context.Context, sessionID, text string) (*Response, bool) {
	if rt.commands == nil {
		return nil, false
	}
	cmdName, cmdArgs := command.DetectCommand(text)
	if cmdName == "" {
		return nil, false
	}
	handler, ok := rt.directCommands[cmdName]
	if !ok {
		return nil, false
	}
	resp, reply := handler(ctx, sessionID, cmdArgs)

	resp.RunID = sessionID + "-direct-" + fmt.Sprintf("%d", time.Now().UnixNano())
	resp.RunStatus = "completed"

	rt.logger.Info("direct command handled",
		zap.String("cmd", cmdName),
		zap.String("args", cmdArgs),
		zap.Int("reply_len", len(reply)))

	return &resp, true
}
