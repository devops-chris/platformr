package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
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
	"github.com/devops-chris/platformr/internal/template"
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
	var repoCfgs []*config.RepoConfig
	var currentUser string
	var loadErr error
	_ = spinner.New().
		Title("Loading org config...").
		Action(func() {
			orgCfg, repoCfgs, loadErr = loader.LoadAll(localCfg.ConnectedOrg)
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

	resources := requestResources(repoCfgs)
	printStatusSummary(author, currentUser, prs, resources, mayHaveMore)
	// In the everyone view, always say who opened each request — even when the
	// results happen to all be one person's, that's exactly what's being asked.
	return offerRequestPicker(prs, resources, repoCfgs, currentUser, author == "")
}

// requestResources maps "owner/repo\x00name" to each request's current config,
// for the display name and status_fields of requests status lists. Keyed by the
// repo the PR was opened in (where the request's files land), since request names
// are only unique within one repo's config.
func requestResources(repos []*config.RepoConfig) map[string]config.Resource {
	out := map[string]config.Resource{}
	for _, res := range remote.AllResources(repos) {
		out[strings.ToLower(res.Resolved.Repo)+"\x00"+res.Name] = res
	}
	return out
}

// requestInfo returns pr's request type (the name in its "## <name> request"
// heading), that request's current config, and whether it still exists — a PR
// is permanent, but the request that opened it may since have been renamed or
// removed, in which case callers fall back to the bare name and the PR title.
func requestInfo(pr ghclient.PRStatus, resources map[string]config.Resource) (string, config.Resource, bool) {
	name := parseResourceType(pr.Body)
	res, ok := resources[strings.ToLower(pr.Repo)+"\x00"+name]
	return name, res, ok
}

// requestLabel is the display name for pr's request type, or its bare name if
// that request no longer exists or has no display_name.
func requestLabel(pr ghclient.PRStatus, resources map[string]config.Resource) string {
	name, res, ok := requestInfo(pr, resources)
	if !ok {
		return name
	}
	return res.Label()
}

// requestSummary is what identifies pr in the list: the answers to its
// request's status_fields (from the PR's Details), joined with " · ", or the PR
// title when status_fields isn't set, the request no longer exists, or none of
// those fields has an answer in this PR.
func requestSummary(pr ghclient.PRStatus, resources map[string]config.Resource) string {
	_, res, ok := requestInfo(pr, resources)
	if !ok || len(res.StatusFields) == 0 {
		return pr.Title
	}
	details := map[string]string{}
	for _, kv := range parseDetails(pr.Body) {
		details[kv[0]] = kv[1]
	}
	var parts []string
	for _, f := range res.StatusFields {
		if v := details[f]; v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return pr.Title
	}
	return strings.Join(parts, " · ")
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
func printStatusSummary(author, currentUser string, prs []ghclient.PRStatus, resources map[string]config.Resource, mayHaveMore bool) {
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
	printTypeBreakdown(prs, resources)
}

// printTypeBreakdown is the at-a-glance "what": how many of the listed
// requests are of each kind, by display name, most common first, with how many
// of each are still open.
func printTypeBreakdown(prs []ghclient.PRStatus, resources map[string]config.Resource) {
	type tally struct {
		label       string
		total, open int
	}
	byLabel := map[string]*tally{}
	var order []*tally
	for _, pr := range prs {
		label := requestLabel(pr, resources)
		t := byLabel[label]
		if t == nil {
			t = &tally{label: label}
			byLabel[label] = t
			order = append(order, t)
		}
		t.total++
		if pr.State == "open" {
			t.open++
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].total > order[j].total })

	width := 0
	for _, t := range order {
		if w := len(t.label); w > width {
			width = w
		}
	}
	for _, t := range order {
		line := fmt.Sprintf("    %-*s  %d", width, t.label, t.total)
		if t.open > 0 {
			line += "  " + ui.Warning(fmt.Sprintf("(%d open)", t.open))
		}
		fmt.Println(line)
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
func offerRequestPicker(prs []ghclient.PRStatus, resources map[string]config.Resource, repos []*config.RepoConfig, currentUser string, showAuthor bool) error {
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
			typeWidth, parseResourceType(pr.Body), numWidth, pr.Number, repoPart, authorPart, statusLabel(pr.State), marker, requestSummary(pr, resources))
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

	fmt.Println(renderRequestDetail(prs[selected], resources, currentUser))
	return offerValueReveal(prs[selected], loginHint(prs[selected], resources, repos))
}

// offerValueReveal lets the user fetch and reveal one specific output value
// via lockr, on demand — not every key eagerly. Two reasons: revealing a
// value just from browsing status is more exposure than intended for
// anything sensitive, and per-key IAM permissions mean one key can succeed
// while another is denied — attempting only the one actually asked for keeps
// that independent instead of surfacing every outcome at once.
//
// Before offering anything, it lists the output folder through lockr (names
// only, nothing decrypted) so an expired login, the wrong account, or a stack
// that isn't applied yet is reported up front, instead of after picking a key.
func offerValueReveal(pr ghclient.PRStatus, hint string) error {
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

	folder := path.Dir(kvs[0][2])
	var existing map[string]bool
	var listErr error
	_ = spinner.New().
		Title("Checking your access with lockr...").
		Action(func() {
			existing, listErr = lockrListNames(folder)
		}).
		Run()
	if listErr != nil {
		fmt.Println(lockrProblem(listErr, folder, hint))
		return nil
	}
	if len(existing) == 0 {
		fmt.Println(ui.Warning("Nothing is stored under " + folder + " where you're logged in" + awsProfileNote() + "."))
		fmt.Println(ui.Subtle("  Either it hasn't been applied yet, or you're logged in to a different account or region than this request's (see Details above)."))
		if hint != "" {
			fmt.Println(ui.Subtle("  To log in where it lives: " + hint))
		}
		return nil
	}

	opts := make([]huh.Option[int], len(kvs))
	for i, kv := range kvs {
		label := kv[1]
		if !existing[kv[2]] {
			label += "  " + ui.Subtle("(not there yet)")
		}
		opts[i] = huh.NewOption(label, i)
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

	key, keyPath := kvs[selected][1], kvs[selected][2]
	var value string
	var fetchErr error
	_ = spinner.New().
		Title("Checking lockr...").
		Action(func() {
			value, fetchErr = lockrReadValue(keyPath)
		}).
		Run()

	if fetchErr != nil {
		fmt.Println(lockrProblem(fetchErr, keyPath, hint))
		return nil
	}
	fmt.Println(ui.Success(fmt.Sprintf("%s → %s", key, value)))
	return nil
}

// renderRequestDetail builds a styled, bordered detail box for one request —
// its status, URL, and outputs (rendered as clean key/value lines, not the raw
// markdown they're stored as in the PR body) or a plain note if it has none.
func renderRequestDetail(pr ghclient.PRStatus, resources map[string]config.Resource, currentUser string) string {
	labelStyle := lipgloss.NewStyle().Faint(true).Width(10)
	var b strings.Builder

	heading := fmt.Sprintf("#%d  %s", pr.Number, requestLabel(pr, resources))
	if name := parseResourceType(pr.Body); name != requestLabel(pr, resources) {
		heading += "  " + ui.Subtle("("+name+")")
	}
	fmt.Fprintf(&b, "%s\n\n", ui.SectionHeader(heading))
	fmt.Fprintf(&b, "%s %s\n", labelStyle.Render("Title:"), pr.Title)
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
			if !lockrAvailable() {
				fmt.Fprintf(&b, "\n  %s\n", ui.Subtle("Install lockr to reveal these values from here."))
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

// lockrAvailable reports whether the lockr binary is on PATH. Fetching values
// is entirely opt-in-by-presence: if lockr isn't installed, platformr shows the
// paths and a one-line hint that lockr would let you reveal them here.
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
	out, err := runLockr("read", path, "--output", "json")
	if err != nil {
		return "", err
	}
	var secret lockrSecret
	if err := json.Unmarshal(lockrJSON(out), &secret); err != nil {
		return "", fmt.Errorf("unexpected lockr output: %w", err)
	}
	return secret.Value, nil
}

// lockrListNames returns the names lockr finds directly under folder, via
// `lockr list <folder> --output json` — metadata only, lockr doesn't decrypt
// anything for a list, so this checks access without revealing a value. An
// empty folder isn't an error: lockr prints a "No secrets found" line instead
// of JSON, which comes back here as an empty set.
func lockrListNames(folder string) (map[string]bool, error) {
	out, err := runLockr("list", folder, "--output", "json")
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	var items []struct {
		Name string `json:"name"`
	}
	if clean := lockrJSON(out); len(clean) > 0 && clean[0] == '[' && json.Unmarshal(clean, &items) == nil {
		for _, it := range items {
			names[it.Name] = true
		}
	}
	return names, nil
}

// ansiEscape matches terminal escape codes: CSI ("\x1b[?25l", colors, cursor moves) and
// OSC ("\x1b]11;?\x1b\\").
var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// lockrJSON returns the JSON part of lockr's stdout. lockr can draw its "Fetching
// secrets…" spinner on stdout even when that isn't a terminal, and the spinner's escape
// codes start with "\x1b[", so looking for the first "[" found the spinner, not the JSON.
// Escape codes are removed first; the JSON then starts at the first "[" or "{".
func lockrJSON(out []byte) []byte {
	clean := ansiEscape.ReplaceAll(out, nil)
	if i := bytes.IndexAny(clean, "[{"); i >= 0 {
		return clean[i:]
	}
	return clean
}

// runLockr runs lockr with args and returns its stdout. On failure the error is
// lockr's own message, in full — the part of an AWS error that says what's
// actually wrong is usually at the end.
func runLockr(args ...string) ([]byte, error) {
	cmd := exec.Command("lockr", args...)
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
		return nil, fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

// lockrProblem turns a lockr failure into what to do about it. A credentials
// failure (expired SSO session, no profile) says so plainly, since lockr's own
// wording for it ("get identity: get credentials: ...") doesn't; anything else
// (access denied, not found) is lockr's message as is. Either way the full
// original text follows, never shortened.
//
// hint is the request's rendered login_hint, if the platform team set one; it
// replaces the generic "log in to the account in Details" advice.
func lockrProblem(err error, path, hint string) string {
	msg := err.Error()
	if isCredentialsError(msg) {
		advice := "  Log in to the account this request is for (see Details above), then run status again."
		if hint != "" {
			advice = "  To log in: " + hint + ", then run status again."
		}
		return ui.Warning("You're not logged in to AWS"+awsProfileNote()+", or your session has expired.") + "\n" +
			ui.Subtle(advice) + "\n" +
			ui.Subtle("  lockr said: "+msg)
	}
	return ui.Warning("lockr couldn't read "+path+":") + "\n  " + msg
}

// isCredentialsError reports whether an AWS SDK error (as relayed by lockr) is
// about having no usable credentials at all, rather than about the request.
func isCredentialsError(msg string) bool {
	for _, s := range []string{"get credentials", "get identity", "ExpiredToken", "SSO", "NoCredentialProviders", "failed to refresh cached credentials"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// loginHint renders pr's request's login_hint with the answers from its PR
// Details (and the repo's maps), or "" if it has none or the request no longer
// exists.
func loginHint(pr ghclient.PRStatus, resources map[string]config.Resource, repos []*config.RepoConfig) string {
	_, res, ok := requestInfo(pr, resources)
	if !ok || res.LoginHint == "" {
		return ""
	}
	values := map[string]string{}
	for _, kv := range parseDetails(pr.Body) {
		values[kv[0]] = kv[1]
	}
	return strings.TrimSpace(template.RenderString(res.LoginHint, values, remote.MapsFor(res, repos)))
}

// awsProfileNote names the AWS profile lockr will use, if one is set, so a
// "wrong account" or "expired" message says which login it means.
func awsProfileNote() string {
	if p := os.Getenv("AWS_PROFILE"); p != "" {
		return " (AWS profile " + p + ")"
	}
	return ""
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
