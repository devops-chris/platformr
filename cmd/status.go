package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// defaultStatusLimit caps how many requests are shown by default so the list
// stays readable as history grows. --all bypasses it.
const defaultStatusLimit = 20

// outputsHeading marks the start of the PR-body section outputSectionMarkdown
// (cmd/request.go) appends — kept in one place so both sides of the contract
// (writing it, reading it back) can't drift from each other.
const outputsHeading = "### Outputs"

var (
	// resourceTypeRe reads the "## <type> request" heading buildPRBody always
	// writes — a structural convention of the body format itself, not a
	// per-resource field name, so it holds regardless of what fields a given
	// resource or IaC tool actually defines.
	resourceTypeRe   = regexp.MustCompile(`^## (\S+) request`)
	outputPrefixRe   = regexp.MustCompile("written under: `(.+?)`")
	outputKeyRe      = regexp.MustCompile("(?m)^- \\*\\*(.+?)\\*\\*: `(.+?)`$")
	outputPlatformRe = regexp.MustCompile(`(?m)^Platform: (\S+)`)
	detailLineRe     = regexp.MustCompile(`(?m)^- \*\*(.+?)\*\*: (.*)$`)
)

var (
	statusAll      bool
	statusUser     string
	statusEveryone bool
)

var statusCmd = &cobra.Command{
	Use:   "status [search words]",
	Short: "Check the status of your requests (or anyone's)",
	Long: `Lists requests opened via ` + "`platformr request`" + ` and whether each is open or
merged, newest first. Pick one to see its details and outputs.

Two separate questions, two separate flags:
  WHOSE requests:  yours by default, -u <login> for one person, -e for everyone
  WHICH requests:  the 20 most recent open/merged by default, -a for the full
                   history including closed-without-merge

Search words narrow the list on GitHub's side (matched against each request's
title and details), so it stays fast however many requests exist.

You see whatever your GitHub read access to the IaC repos already shows you.
Revealing an output value still goes through lockr and your own cloud permissions.`,
	Example: `  platformr status                 # your 20 most recent requests
  platformr status -a              # all of yours, including closed
  platformr status -e              # everyone's 20 most recent
  platformr status -e orders       # everyone's requests mentioning "orders"
  platformr status -u alex rds     # alex's requests mentioning "rds"`,
	Args: cobra.ArbitraryArgs,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVarP(&statusAll, "all", "a", false, fmt.Sprintf(
		"Full history: include closed-without-merge and drop the %d-request limit", defaultStatusLimit))
	statusCmd.Flags().StringVarP(&statusUser, "user", "u", "", "Show this GitHub login's requests instead of yours")
	statusCmd.Flags().BoolVarP(&statusEveryone, "everyone", "e", false, "Show everyone's requests, not just yours")
	statusCmd.MarkFlagsMutuallyExclusive("user", "everyone")
}

func runStatus(cmd *cobra.Command, args []string) error {
	binaryName := filepath.Base(os.Args[0])
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fmt.Println(ui.Warning("Not connected. Run `" + binaryName + " connect <org>` first."))
		os.Exit(1)
	}

	token := resolveReadToken()
	loader := remote.New(token)
	gh := ghclient.New(token)

	var orgCfg *config.OrgConfig
	var currentUser string
	var loadErr error
	_ = spinner.New().
		Title("Loading org config...").
		Action(func() {
			orgCfg, _, loadErr = loader.LoadAll(localCfg.ConnectedOrg)
			if loadErr != nil {
				return
			}
			currentUser, loadErr = gh.CurrentUser()
		}).
		Run()
	if loadErr != nil {
		return fmt.Errorf("loading org config: %w", loadErr)
	}

	// Collect the set of repos ever registered in the org config directly — not
	// derived from currently-resolved resources. A request's PR is permanent
	// regardless of whether today's platformr.toml on today's ref still defines
	// the resource that created it, so repo discovery here must not depend on
	// that resolving successfully (or on any --ref override at all).
	targetRepos := map[string]bool{}
	for _, r := range orgCfg.Repos {
		repoURL := remote.ResolveRepoURL(r.URL, orgCfg.GitHub.DefaultOrg)
		targetRepos[repoURL] = true
	}

	// Bound the per-repo fetch to what we'd actually display, unless --all was
	// passed — results come back newest-first, so this is a correct "N most
	// recent", not an arbitrary truncation, and avoids paying for full history
	// pagination just to throw most of it away in the common case.
	perRepoLimit := defaultStatusLimit
	if statusAll {
		perRepoLimit = 0
	}

	// Whose requests to search for — "" means anyone (--everyone).
	author := currentUser
	if statusUser != "" {
		author = strings.TrimPrefix(statusUser, "@")
	} else if statusEveryone {
		author = ""
	}
	search := searchQuery(args)

	var prs []ghclient.PRStatus
	var mayHaveMore bool
	var fetchErr error
	_ = spinner.New().
		Title("Checking PR status...").
		Action(func() {
			for repo := range targetRepos {
				repoPRs, err := gh.RequestPRs(repo, author, search, perRepoLimit, statusAll)
				if err != nil {
					fetchErr = err
					return
				}
				// Hitting the per-repo cap means this repo alone may have more
				// than we fetched — an honest signal, not a precise count.
				if !statusAll && len(repoPRs) == perRepoLimit {
					mayHaveMore = true
				}
				prs = append(prs, repoPRs...)
			}
		}).
		Run()
	if fetchErr != nil {
		return fmt.Errorf("checking PR status: %w", fetchErr)
	}

	if len(prs) == 0 {
		msg := "No requests found"
		if author != "" {
			msg += " for " + author
		}
		if search != "" {
			msg += " matching " + search
		}
		fmt.Println(ui.Subtle(msg + "."))
		return nil
	}

	sort.Slice(prs, func(i, j int) bool { return prs[i].CreatedAt.After(prs[j].CreatedAt) })

	if !statusAll && len(prs) > defaultStatusLimit {
		mayHaveMore = true
		prs = prs[:defaultStatusLimit]
	}

	printStatusSummary(author, currentUser, prs, mayHaveMore)
	// In the everyone view, always say who opened each request — even when the
	// results happen to all be one person's, that's exactly what's being asked.
	return offerRequestPicker(prs, currentUser, author == "")
}

// searchQuery joins status's search words into GitHub search free text. The
// shell has already stripped any quotes, so an argument that still contains a
// space was quoted on purpose (status -e "billing db") and is re-quoted to stay
// an exact phrase rather than becoming two separate words.
func searchQuery(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") && !strings.Contains(a, `"`) {
			a = `"` + a + `"`
		}
		words[i] = a
	}
	return strings.Join(words, " ")
}

// printStatusSummary prints one line of counts — no table. With more than a
// handful of requests, a full table and a searchable picker end up showing the
// same rows twice; the picker below is the one place requests are actually
// listed, and it's searchable (type/name, or just part of either) instead of
// something you scroll through.
//
// author is whose requests were searched for ("" for everyone); currentUser
// decides whether that reads as "Your requests" or someone else's.
func printStatusSummary(author, currentUser string, prs []ghclient.PRStatus, mayHaveMore bool) {
	open, merged, closed := 0, 0, 0
	for _, pr := range prs {
		switch pr.State {
		case "open":
			open++
		case "merged":
			merged++
		default:
			closed++
		}
	}
	counts := fmt.Sprintf("%d open, %d merged", open, merged)
	if closed > 0 {
		// Only ever non-zero with --all — the default fetch already excludes
		// closed-without-merge requests entirely.
		counts += fmt.Sprintf(", %d closed", closed)
	}
	heading, whose := "Your requests", "your"
	switch author {
	case "":
		heading, whose = "All requests", "the"
	case currentUser:
	default:
		heading, whose = author+"'s requests", author+"'s"
	}
	who := ""
	if author != "" {
		who = "  " + ui.Subtle(author)
	}
	fmt.Printf("\n  %s%s — %s\n", ui.SectionHeader(heading), who, counts)
	if mayHaveMore {
		fmt.Println(ui.Subtle(fmt.Sprintf("  Showing %s %d most recent — add search words to narrow, or -a for the full history.", whose, len(prs))))
	}
	fmt.Println()
}

// offerRequestPicker is the one place requests are listed — searchable via huh's
// builtin filter (press "/", type any part of the resource type, the PR title,
// or a PR number). Deliberately doesn't assume any particular resource field
// (like "account") exists — the PR title is the one thing that's always
// present and already the platform team's customizable convention for saying
// whatever's meaningful per resource type (see pr_title in platformr.toml), so
// that's what's used to identify and disambiguate requests here, not a
// hardcoded field name that not every resource or IaC tool will have.
// Selecting shows a styled detail view with status and outputs together, since
// checking either is the actual reason to look this up.
func offerRequestPicker(prs []ghclient.PRStatus, currentUser string, showAuthor bool) error {
	// PR numbers are only unique per-repo — a requestor may not even be aware
	// there's more than one registered IaC repo. Only disambiguate when it's
	// actually ambiguous, so the common single-repo case stays uncluttered.
	// Same reasoning for authors: show who opened each request when asked to
	// (the everyone view) or when the list mixes people; otherwise the heading
	// already says whose.
	multiRepo, multiAuthor := false, showAuthor
	for _, pr := range prs {
		if pr.Repo != prs[0].Repo {
			multiRepo = true
		}
		if pr.Author != prs[0].Author {
			multiAuthor = true
		}
	}

	// Column widths computed from this list, not hardcoded — the title is the
	// one genuinely unbounded field, so it goes last (nothing needs to align
	// after it, same reasoning as why `git log --oneline` doesn't try to align
	// commit subjects). Everything before it is bounded and gets padded so it
	// actually lines up, unlike title, which was previously stuck in the
	// middle of the line with no padding of its own.
	typeWidth, numWidth, repoWidth, authorWidth := 0, 0, 0, 0
	for _, pr := range prs {
		if w := len(parseResourceType(pr.Body)); w > typeWidth {
			typeWidth = w
		}
		if w := len(strconv.Itoa(pr.Number)); w > numWidth {
			numWidth = w
		}
		if w := len(repoDisambiguator(pr.Repo)); w > repoWidth {
			repoWidth = w
		}
		if w := len(pr.Author); w > authorWidth {
			authorWidth = w
		}
	}

	opts := make([]huh.Option[int], len(prs))
	for i, pr := range prs {
		marker := "         " // 9 spaces — reserves the "✓ outputs" width when absent
		if parseOutputsSection(pr.Body) != "" {
			marker = "✓ outputs"
		}
		repoPart := ""
		if multiRepo {
			repoPart = fmt.Sprintf("[%-*s]  ", repoWidth, repoDisambiguator(pr.Repo))
		}
		authorPart := ""
		if multiAuthor {
			authorPart = fmt.Sprintf("%-*s  ", authorWidth, pr.Author)
		}
		label := fmt.Sprintf("%-*s  #%-*d  %s%s%s  %s  %s",
			typeWidth, parseResourceType(pr.Body), numWidth, pr.Number, repoPart, authorPart, statusLabel(pr.State), marker, pr.Title)
		opts[i] = huh.NewOption(label, i)
	}
	opts = append(opts, huh.NewOption("— skip —", -1))

	var selected int
	sel := huh.NewSelect[int]().
		Title("Find a request").
		Description("Press / then type to search — e.g. a resource type, a word from its title, a PR number, or a login.").
		Options(opts...).
		Value(&selected)
	sel.WithTheme(ui.Theme())
	if err := sel.Run(); err != nil {
		return err
	}
	if selected == -1 {
		return nil
	}

	fmt.Println(renderRequestDetail(prs[selected], currentUser))
	return offerValueReveal(prs[selected])
}

// offerValueReveal lets the user fetch and reveal one specific output value
// via lockr, on demand — not every key eagerly. Two reasons: revealing a
// value just from browsing status is more exposure than intended for
// anything sensitive, and per-key IAM permissions mean one key can succeed
// while another is denied — attempting only the one actually asked for keeps
// that independent instead of surfacing every outcome at once.
func offerValueReveal(pr ghclient.PRStatus) error {
	if !lockrAvailable() {
		return nil
	}
	section := parseOutputsSection(pr.Body)
	if section == "" {
		return nil
	}
	kvs := outputKeyRe.FindAllStringSubmatch(section, -1)
	if len(kvs) == 0 {
		return nil
	}

	opts := make([]huh.Option[int], len(kvs))
	for i, kv := range kvs {
		opts[i] = huh.NewOption(kv[1], i)
	}
	opts = append(opts, huh.NewOption("— skip —", -1))

	var selected int
	sel := huh.NewSelect[int]().
		Title("Reveal a value via lockr?").
		Options(opts...).
		Value(&selected)
	sel.WithTheme(ui.Theme())
	if err := sel.Run(); err != nil {
		return err
	}
	if selected == -1 {
		return nil
	}

	key, path := kvs[selected][1], kvs[selected][2]
	var value string
	var fetchErr error
	_ = spinner.New().
		Title("Checking lockr...").
		Action(func() {
			value, fetchErr = lockrReadValue(path)
		}).
		Run()

	if fetchErr != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("%s: %s", key, truncate(fetchErr.Error(), 80))))
		return nil
	}
	fmt.Println(ui.Success(fmt.Sprintf("%s → %s", key, value)))
	return nil
}

// renderRequestDetail builds a styled, bordered detail box for one request —
// its status, URL, and outputs (rendered as clean key/value lines, not the raw
// markdown they're stored as in the PR body) or a plain note if it has none.
func renderRequestDetail(pr ghclient.PRStatus, currentUser string) string {
	labelStyle := lipgloss.NewStyle().Faint(true).Width(10)
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\n", ui.SectionHeader(fmt.Sprintf("#%d  [%s]  %s", pr.Number, parseResourceType(pr.Body), pr.Title)))
	fmt.Fprintf(&b, "%s %s\n", labelStyle.Render("Status:"), statusLabel(pr.State))
	if pr.Author != currentUser {
		fmt.Fprintf(&b, "%s %s\n", labelStyle.Render("By:"), pr.Author)
	}
	fmt.Fprintf(&b, "%s %s\n", labelStyle.Render("Repo:"), pr.Repo)
	fmt.Fprintf(&b, "%s %s\n\n", labelStyle.Render("URL:"), hyperlink(pr.URL))

	// Whatever fields this resource actually has — no assumption that "account"
	// or any other specific field exists. Shown because you need to know which
	// account/subscription/project to be authenticated into (e.g. via cloudctx)
	// before a lockr fetch above will succeed, and that context varies by
	// resource type, so it can't be hardcoded here.
	if details := parseDetails(pr.Body); len(details) > 0 {
		b.WriteString(ui.SectionHeader("Details") + "\n\n")
		keyWidth := 0
		for _, kv := range details {
			if w := len(kv[0]); w > keyWidth {
				keyWidth = w
			}
		}
		for _, kv := range details {
			fmt.Fprintf(&b, "  %-*s %s\n", keyWidth, kv[0], kv[1])
		}
		b.WriteString("\n")
	}

	section := parseOutputsSection(pr.Body)
	if section == "" {
		b.WriteString(ui.Subtle("No outputs configured for this resource."))
	} else {
		heading := "Outputs"
		if m := outputPlatformRe.FindStringSubmatch(section); m != nil {
			heading += " " + ui.Subtle("("+m[1]+")")
		}
		if kvs := outputKeyRe.FindAllStringSubmatch(section, -1); len(kvs) > 0 {
			b.WriteString(ui.SectionHeader(heading) + "\n\n")
			if pr.State == "open" {
				fmt.Fprintf(&b, "  %s\n\n", ui.Warning("Still open — nothing's been applied yet, so these likely don't exist to fetch."))
			}
			for _, kv := range kvs {
				fmt.Fprintf(&b, "  %-14s %s\n", kv[1], kv[2])
			}
		} else if m := outputPrefixRe.FindStringSubmatch(section); m != nil {
			b.WriteString(ui.SectionHeader(heading) + "\n\n")
			if pr.State == "open" {
				fmt.Fprintf(&b, "  %s\n\n", ui.Warning("Still open — nothing's been applied yet, so these likely don't exist to fetch."))
			}
			fmt.Fprintf(&b, "  %s\n", m[1])
		}
	}

	return lipgloss.NewStyle().
		MarginLeft(2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPurple).
		Padding(1, 2).
		Render(strings.TrimRight(b.String(), "\n"))
}

// truncate shortens s for single-line display, e.g. a verbose chained error
// message from an external tool. Callers needing the full text (logs,
// returned errors) should use the untruncated value instead.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// lockrAvailable reports whether the lockr binary is on PATH. Fetching values
// is entirely opt-in-by-presence: if lockr isn't installed, platformr just
// shows the path, same as if this feature didn't exist at all.
func lockrAvailable() bool {
	_, err := exec.LookPath("lockr")
	return err == nil
}

// lockrSecret mirrors lockr's --output json shape for `lockr read`.
type lockrSecret struct {
	Value string `json:"value"`
}

// lockrReadValue shells out to `lockr read <path> --output json`. platformr
// never talks to AWS/SSM directly and never assumes a specific backend —
// it only ever calls lockr and parses its output, so this rides along with
// whatever backends lockr itself ends up supporting (see output_cloud).
// Returns lockr's own first-line error message on failure (permission
// denied, not applied yet, expired credentials, whatever it actually is)
// rather than swallowing or reinterpreting it.
func lockrReadValue(path string) (string, error) {
	cmd := exec.Command("lockr", "read", path, "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if lines := strings.SplitN(msg, "\n", 2); len(lines) > 0 && lines[0] != "" {
			msg = strings.TrimPrefix(lines[0], "Error: ")
		}
		return "", fmt.Errorf("%s", msg)
	}
	var secret lockrSecret
	if err := json.Unmarshal(stdout.Bytes(), &secret); err != nil {
		return "", fmt.Errorf("unexpected lockr output: %w", err)
	}
	return secret.Value, nil
}

// parseOutputsSection extracts the "### Outputs" section written by
// outputSectionMarkdown (cmd/request.go) from a PR body. Returns "" if the PR
// has no such section — either output_path wasn't configured for that
// resource, or this PR predates the feature.
func parseOutputsSection(body string) string {
	idx := strings.Index(body, outputsHeading)
	if idx == -1 {
		return ""
	}
	section := body[idx+len(outputsHeading):]
	return strings.TrimSpace(section)
}

// parseResourceType extracts the resource type from the "## <type> request"
// heading buildPRBody (cmd/request.go) always writes as the first line.
func parseResourceType(body string) string {
	m := resourceTypeRe.FindStringSubmatch(body)
	if m == nil {
		return "?"
	}
	return m[1]
}

// parseDetails extracts every "- **key**: value" line from the PR body's
// Details section (the section buildPRBody always writes from the request's
// field values), sorted alphabetically for a stable, scannable order — the
// order fields originally appear in the body isn't meaningful, since
// buildPRBody writes them by ranging a Go map. Deliberately generic: doesn't
// assume "account" or any other specific field exists, since that varies by
// resource type and IaC tool.
func parseDetails(body string) [][2]string {
	start := strings.Index(body, "### Details")
	if start == -1 {
		return nil
	}
	section := body[start:]
	if next := strings.Index(section[len("### Details"):], "\n### "); next != -1 {
		section = section[:len("### Details")+next]
	}

	matches := detailLineRe.FindAllStringSubmatch(section, -1)
	out := make([][2]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, [2]string{m[1], strings.TrimSpace(m[2])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// repoDisambiguator shortens a "owner/repo" string for display by dropping the
// connected org's own prefix when it matches (the common case), so same-org
// multi-repo disambiguation stays short; a cross-org repo keeps its full
// "owner/repo" since dropping the prefix there would be actively misleading.
func repoDisambiguator(repo string) string {
	if localCfg != nil && localCfg.ConnectedOrg != "" {
		if short := strings.TrimPrefix(repo, localCfg.ConnectedOrg+"/"); short != repo {
			return short
		}
	}
	return repo
}

// hyperlink renders url as a clickable OSC8 terminal hyperlink, showing the
// URL itself as the link text — no information lost for terminals that don't
// support it (Ghostty, iTerm2, Kitty, WezTerm, VSCode, Windows Terminal, and
// most others do); they just render the same visible text with no link.
func hyperlink(url string) string {
	return termenv.Hyperlink(url, url)
}

// statusLabel returns a colored, fixed-width state word — padded to the
// widest of the three ("Merged"/"Closed", 6 chars) *before* styling, so
// anything placed after it in a Sprintf still lines up. Padding the already-
// styled string wouldn't work: the width verb would count invisible ANSI
// bytes along with the visible text, throwing off the padding itself.
func statusLabel(state string) string {
	switch state {
	case "open":
		return ui.Warning(fmt.Sprintf("%-6s", "Open"))
	case "merged":
		return ui.Success(fmt.Sprintf("%-6s", "Merged"))
	default:
		return ui.Subtle(fmt.Sprintf("%-6s", "Closed"))
	}
}
