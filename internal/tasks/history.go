package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var terminalEscape = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\))")

func readable(s string) string {
	s = terminalEscape.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

type historyCursor struct {
	Version int    `json:"v"`
	Task    string `json:"task"`
	Turn    string `json:"turn,omitempty"`
	Outputs bool   `json:"outputs,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
}

func (s *service) items(ctx context.Context, o Options, r *Result) error {
	pos := historyCursor{Version: 1, Task: o.TaskID, Turn: o.TurnID, Outputs: o.IncludeOutputs}
	if o.Cursor != "" {
		if err := decodeCursor(o.Cursor, &pos); err != nil {
			return err
		}
		if pos.Version != 1 || pos.Task != o.TaskID || pos.Turn != o.TurnID || pos.Outputs != o.IncludeOutputs {
			return fmt.Errorf("history cursor belongs to another task, turn, or output filter")
		}
	}
	maxChars := o.MaxChars
	if maxChars == 0 {
		maxChars = 4000
	}
	limit := o.Limit
	if limit == 0 {
		limit = 20
	}
	if o.ItemID != "" {
		found := false
		r.ItemFound = &found
	}
	// Single-item upstream cursors preserve exact continuation positions even
	// while a task grows. Bound sparse-history scans and return a continuation.
	for scanned := 0; scanned < 200; scanned++ {
		before := pos
		var page struct {
			Data []struct {
				TurnID string          `json:"turnId"`
				Item   json.RawMessage `json:"item"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		params := map[string]any{"threadId": o.TaskID, "limit": 1, "cursor": nullable(pos.Cursor), "sortDirection": "desc"}
		if o.TurnID != "" {
			params["turnId"] = o.TurnID
		}
		if err := s.call(ctx, "thread/items/list", params, &page, false); err != nil {
			r.NextCursor = encodeCursor(before)
			return err
		}
		if len(page.Data) > 1 {
			return fmt.Errorf("history server ignored its requested page bound")
		}
		if page.NextCursor != "" && page.NextCursor == pos.Cursor {
			return fmt.Errorf("history pagination did not advance")
		}
		pos.Cursor = page.NextCursor
		r.NextCursor = ""
		if pos.Cursor != "" {
			r.NextCursor = encodeCursor(pos)
		}
		for _, entry := range page.Data {
			var raw struct {
				ID      string          `json:"id"`
				Type    string          `json:"type"`
				Text    string          `json:"text"`
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(entry.Item, &raw); err != nil {
				return fmt.Errorf("invalid history item: %w", err)
			}
			if o.ItemID != "" && raw.ID != o.ItemID {
				continue
			}
			if r.ItemFound != nil {
				*r.ItemFound = true
			}
			i := Item{ID: raw.ID, TurnID: entry.TurnID, Type: raw.Type}
			switch raw.Type {
			case "userMessage", "agentMessage", "plan":
				parts := []string{}
				if raw.Text != "" {
					parts = append(parts, raw.Text)
				}
				var content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if len(raw.Content) > 0 {
					if err := json.Unmarshal(raw.Content, &content); err != nil {
						return fmt.Errorf("invalid message content: %w", err)
					}
				}
				for _, part := range content {
					if part.Type == "text" {
						parts = append(parts, part.Text)
					} else {
						i.OmittedParts++
					}
				}
				i.Text = strings.Join(parts, "\n")
			case "commandExecution", "mcpToolCall", "dynamicToolCall", "fileChange", "webSearch":
				if o.IncludeOutputs {
					i.Text = string(entry.Item)
				} else {
					r.OmittedItems++
					continue
				}
			default:
				r.OmittedItems++
				continue
			}
			runes := []rune(readable(i.Text))
			if o.Offset > len(runes) {
				return fmt.Errorf("offset exceeds item length (%d characters)", len(runes))
			}
			end := o.Offset + maxChars
			if end < len(runes) {
				i.Truncated, i.NextOffset, i.ContinuationCursor = true, end, encodeCursor(before)
			} else {
				end = len(runes)
			}
			i.Text = string(runes[o.Offset:end])
			r.Items = append(r.Items, i)
		}
		if o.ItemID != "" && *r.ItemFound {
			return nil
		}
		if len(r.Items) >= limit || pos.Cursor == "" {
			return nil
		}
	}
	return nil
}
