package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"grubby/internal/github"
)

type MentionType int

const (
	MentionUser MentionType = iota
	MentionRepo
	MentionPR
)

type Mention struct {
	Type  MentionType
	Value string // e.g. "simon", "grubby", "15"
	Repo  string // e.g. "grubby" if qualified PR
	Raw   string // e.g. "@simon", "#grubby", "!15", "grubby#15"
	Start int
	End   int
}

var (
	mdLinkRegex       = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s)]+)\)`)
	urlRegex          = regexp.MustCompile(`https?://[^\s)\]>"']+`)
	userRegex         = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)
	repoPRRegex       = regexp.MustCompile(`(?:^|\s)#?([a-zA-Z0-9_.-]+)#(\d+)`)
	repoRegex         = regexp.MustCompile(`(?:^|\s)#([a-zA-Z0-9_.-]+)`)
	prRegex           = regexp.MustCompile(`(?:^|\s)(?:!|#)(\d+)`)
	mdInlineCodeRegex = regexp.MustCompile("`([^`\n]+)`")
	mdBoldRegex       = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdItalicRegex     = regexp.MustCompile(`\*([^*\n]+)\*`)
)

// YankableItem represents an item extracted from a message that can be yanked to clipboard.
type YankableItem struct {
	Index int    // 1-based index for selection
	Label string // Display label (e.g. "https://...", "#grubby", "@alice")
	Value string // Value copied to clipboard (always the actual URL)
}

// ExtractYankables extracts URLs, markdown links, repos, PRs, and users from message text for clipboard yanking.
// It ensures that whatever item is extracted, Value is always the actual URL.
func ExtractYankables(text string, org string) []YankableItem {
	var items []YankableItem
	seen := make(map[string]bool)

	// 1. Markdown Links [label](https://...)
	for _, match := range mdLinkRegex.FindAllStringSubmatch(text, -1) {
		if len(match) >= 3 {
			label := strings.TrimSpace(match[1])
			url := strings.TrimRight(match[2], ".,!?:;)")
			if url != "" && !seen[url] {
				seen[url] = true
				seen[label] = true
				items = append(items, YankableItem{
					Index: len(items) + 1,
					Label: label,
					Value: url,
				})
			}
		}
	}

	// 2. Full URLs
	for _, match := range urlRegex.FindAllString(text, -1) {
		cleaned := strings.TrimRight(match, ".,!?:;)")
		if cleaned != "" && !seen[cleaned] {
			seen[cleaned] = true
			items = append(items, YankableItem{
				Index: len(items) + 1,
				Label: cleaned,
				Value: cleaned,
			})
		}
	}

	// 3. Mentions (#repo, !pr, @user)
	mentions := ExtractMentions(text)
	for _, mn := range mentions {
		label := mn.Raw
		var actualURL string
		switch mn.Type {
		case MentionUser:
			actualURL = fmt.Sprintf("https://github.com/%s", mn.Value)
		case MentionRepo:
			if org != "" {
				actualURL = fmt.Sprintf("https://github.com/%s/%s", org, mn.Value)
			} else {
				actualURL = fmt.Sprintf("https://github.com/%s", mn.Value)
			}
		case MentionPR:
			if mn.Repo != "" {
				if org != "" {
					actualURL = fmt.Sprintf("https://github.com/%s/%s/pull/%s", org, mn.Repo, mn.Value)
				} else {
					actualURL = fmt.Sprintf("https://github.com/%s/pull/%s", mn.Repo, mn.Value)
				}
			} else if org != "" {
				actualURL = fmt.Sprintf("https://github.com/%s/pull/%s", org, mn.Value)
			} else {
				actualURL = fmt.Sprintf("https://github.com/pull/%s", mn.Value)
			}
		}

		if actualURL != "" && !seen[actualURL] && !seen[label] {
			seen[actualURL] = true
			seen[label] = true
			items = append(items, YankableItem{
				Index: len(items) + 1,
				Label: label,
				Value: actualURL,
			})
		}
	}

	return items
}

// renderMarkdownContent formats markdown links, inline code, bold, and mentions for terminal display.
func renderMarkdownContent(text string) string {
	if text == "" {
		return ""
	}

	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#56B6C2")).Underline(true)
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")).Background(lipgloss.Color("#282C34"))
	boldStyle := lipgloss.NewStyle().Bold(true)
	italicStyle := lipgloss.NewStyle().Italic(true)
	userStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)
	repoStyle := lipgloss.NewStyle().Foreground(SecondaryColor)
	prStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D19A66"))
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF")).Underline(true)

	placeholders := make(map[string]string)
	pIdx := 0

	// 1. Markdown links [label](url)
	text = mdLinkRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := mdLinkRegex.FindStringSubmatch(match)
		if len(sub) >= 3 {
			label := sub[1]
			url := sub[2]
			token := fmt.Sprintf("\x00MDLINK_%d\x00", pIdx)
			pIdx++
			styled := linkStyle.Render(label)
			hyperlink := fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, styled)
			placeholders[token] = hyperlink
			return token
		}
		return match
	})

	// 2. Raw URLs (https://...)
	text = urlRegex.ReplaceAllStringFunc(text, func(match string) string {
		token := fmt.Sprintf("\x00URL_%d\x00", pIdx)
		pIdx++
		styled := urlStyle.Render(match)
		hyperlink := fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", match, styled)
		placeholders[token] = hyperlink
		return token
	})

	// 3. Inline code `code`
	text = mdInlineCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := mdInlineCodeRegex.FindStringSubmatch(match)
		if len(sub) >= 2 {
			token := fmt.Sprintf("\x00CODE_%d\x00", pIdx)
			pIdx++
			placeholders[token] = codeStyle.Render(" " + sub[1] + " ")
			return token
		}
		return match
	})

	// 4. Bold **text**
	text = mdBoldRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := mdBoldRegex.FindStringSubmatch(match)
		if len(sub) >= 2 {
			return boldStyle.Render(sub[1])
		}
		return match
	})

	// 5. Italic *text*
	text = mdItalicRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := mdItalicRegex.FindStringSubmatch(match)
		if len(sub) >= 2 {
			return italicStyle.Render(sub[1])
		}
		return match
	})

	// 6. Standalone User mentions @user
	text = userRegex.ReplaceAllStringFunc(text, func(match string) string {
		return userStyle.Render(match)
	})

	// 7. Standalone Repo PR mentions repo#123 or #repo#123
	text = repoPRRegex.ReplaceAllStringFunc(text, func(match string) string {
		return prStyle.Render(match)
	})

	// 8. Standalone PR mentions #123 or !123
	text = prRegex.ReplaceAllStringFunc(text, func(match string) string {
		return prStyle.Render(match)
	})

	// 9. Standalone Repo mentions #repo
	text = repoRegex.ReplaceAllStringFunc(text, func(match string) string {
		trimmed := strings.TrimSpace(match)
		if strings.HasPrefix(trimmed, "#") {
			name := trimmed[1:]
			if _, err := strconv.Atoi(name); err == nil {
				return prStyle.Render(match)
			}
		}
		return repoStyle.Render(match)
	})

	// 10. Restore placeholders
	for token, repl := range placeholders {
		text = strings.ReplaceAll(text, token, repl)
	}

	return text
}

// ExtractThreadYankables extracts all unique links and mentions across an entire thread (OP + replies).
func ExtractThreadYankables(disc *github.Discussion, comments []github.DiscussionComment, org string) []YankableItem {
	var items []YankableItem
	seen := make(map[string]bool)

	collect := func(text string) {
		for _, yk := range ExtractYankables(text, org) {
			if !seen[yk.Value] {
				seen[yk.Value] = true
				yk.Index = len(items) + 1
				items = append(items, yk)
			}
		}
	}

	if disc != nil {
		collect(disc.Body)
	}
	for _, cm := range comments {
		collect(cm.Body)
	}

	return items
}

// ExtractMentions extracts users (@user), repositories (#repo), and PRs (!123 or #123) from text.
func ExtractMentions(text string) []Mention {
	var mentions []Mention
	seen := make(map[string]bool)

	// Users (@handle)
	for _, match := range userRegex.FindAllStringSubmatchIndex(text, -1) {
		if len(match) >= 4 {
			val := strings.TrimRight(text[match[2]:match[3]], ".,!?:;")
			raw := text[match[0]:match[1]]
			key := "user:" + strings.ToLower(val)
			if val != "" && !seen[key] {
				seen[key] = true
				mentions = append(mentions, Mention{
					Type:  MentionUser,
					Value: val,
					Raw:   strings.TrimSpace(raw),
					Start: match[0],
					End:   match[1],
				})
			}
		}
	}

	// Qualified repo PRs (repo#123 or #repo#123)
	for _, match := range repoPRRegex.FindAllStringSubmatchIndex(text, -1) {
		if len(match) >= 6 {
			repo := text[match[2]:match[3]]
			val := strings.TrimRight(text[match[4]:match[5]], ".,!?:;")
			raw := strings.TrimSpace(text[match[0]:match[1]])
			key := "pr:" + strings.ToLower(repo) + "#" + val
			if val != "" && !seen[key] {
				seen[key] = true
				mentions = append(mentions, Mention{
					Type:  MentionPR,
					Value: val,
					Repo:  repo,
					Raw:   raw,
					Start: match[0],
					End:   match[1],
				})
			}
		}
	}

	// PRs / Issue numbers (!123 or #123)
	for _, match := range prRegex.FindAllStringSubmatchIndex(text, -1) {
		if len(match) >= 4 {
			val := strings.TrimRight(text[match[2]:match[3]], ".,!?:;")
			raw := strings.TrimSpace(text[match[0]:match[1]])
			key := "pr:" + val
			if val != "" && !seen[key] {
				seen[key] = true
				mentions = append(mentions, Mention{
					Type:  MentionPR,
					Value: val,
					Raw:   raw,
					Start: match[0],
					End:   match[1],
				})
			}
		}
	}

	// Repos (#name)
	for _, match := range repoRegex.FindAllStringSubmatchIndex(text, -1) {
		if len(match) >= 4 {
			val := strings.TrimRight(text[match[2]:match[3]], ".,!?:;")
			if _, err := strconv.Atoi(val); err == nil {
				continue // numeric values are treated as PRs
			}
			raw := strings.TrimSpace(text[match[0]:match[1]])
			key := "repo:" + strings.ToLower(val)
			if val != "" && !seen[key] {
				seen[key] = true
				mentions = append(mentions, Mention{
					Type:  MentionRepo,
					Value: val,
					Raw:   raw,
					Start: match[0],
					End:   match[1],
				})
			}
		}
	}

	// Sort mentions in reading order (by appearance in text)
	for i := 0; i < len(mentions)-1; i++ {
		for j := i + 1; j < len(mentions); j++ {
			if mentions[i].Start > mentions[j].Start {
				mentions[i], mentions[j] = mentions[j], mentions[i]
			}
		}
	}

	return mentions
}
