package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	gh "github.com/google/go-github/v67/github"
	"golang.org/x/oauth2"
)

type Client struct {
	client *gh.Client
}

// IsUnauthorized reports whether err is a GitHub API 401 response — the signature
// of a revoked or expired token (GitHub App user-to-server tokens expire and this
// app doesn't yet refresh them, see auth.LoadToken).
func IsUnauthorized(err error) bool {
	var ghErr *gh.ErrorResponse
	if errors.As(err, &ghErr) {
		return ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusUnauthorized
	}
	return false
}

func New(token string) *Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := gh.NewClient(tc)

	// Support GitHub Enterprise Server via GH_HOST env var (mirrors the gh CLI convention)
	if host := os.Getenv("GH_HOST"); host != "" {
		enterpriseURL := "https://" + host + "/"
		var err error
		client, err = client.WithEnterpriseURLs(enterpriseURL, enterpriseURL)
		if err != nil {
			// Non-fatal — fall back to github.com
			client = gh.NewClient(tc)
		}
	}

	return &Client{client: client}
}

// PRFile is a single file to commit as part of a pull request.
type PRFile struct {
	Path    string
	Content string
}

// TemplateFile is a raw template fetched from a template directory.
type TemplateFile struct {
	Name    string // filename as it appears in GitHub, e.g. "vpc.tf.tmpl"
	Content string
}

type PRRequest struct {
	Repo          string
	Branch        string
	BaseBranch    string
	Title         string
	Body          string
	Files         []PRFile // multi-file commit (use this or FilePath/Content)
	FilePath      string   // single-file legacy
	Content       string   // single-file legacy
	Reviewers     []string // GitHub usernames to request review from
	TeamReviewers []string // GitHub team slugs to request review from
}

// FetchFile fetches the raw string content of a file from a GitHub repo.
// ref is the branch/tag/SHA to fetch from; empty string uses the repo's default branch.
func (c *Client) FetchFile(repo, path, ref string) (string, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return "", err
	}
	var opts *gh.RepositoryContentGetOptions
	if ref != "" {
		opts = &gh.RepositoryContentGetOptions{Ref: ref}
	}
	content, _, _, err := c.client.Repositories.GetContents(context.Background(), owner, repoName, path, opts)
	if err != nil {
		return "", fmt.Errorf("fetching %s from %s: %w", path, repo, err)
	}
	data, err := content.GetContent()
	if err != nil {
		return "", fmt.Errorf("decoding %s from %s: %w", path, repo, err)
	}
	return data, nil
}

// FetchTemplateDir lists and fetches all .tmpl files from a directory in a GitHub repo.
// ref is the branch/tag/SHA to fetch from; empty string uses the repo's default branch.
func (c *Client) FetchTemplateDir(repo, dirPath, ref string) ([]TemplateFile, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}
	var opts *gh.RepositoryContentGetOptions
	if ref != "" {
		opts = &gh.RepositoryContentGetOptions{Ref: ref}
	}
	_, contents, _, err := c.client.Repositories.GetContents(context.Background(), owner, repoName, dirPath, opts)
	if err != nil {
		return nil, fmt.Errorf("listing template dir %s in %s: %w", dirPath, repo, err)
	}
	var files []TemplateFile
	for _, item := range contents {
		if item.GetType() != "file" || !strings.HasSuffix(item.GetName(), ".tmpl") {
			continue
		}
		data, err := c.FetchFile(repo, item.GetPath(), ref)
		if err != nil {
			return nil, fmt.Errorf("fetching template %s: %w", item.GetName(), err)
		}
		files = append(files, TemplateFile{Name: item.GetName(), Content: data})
	}
	return files, nil
}

// ListFiles lists files in a directory and returns their names without extensions.
func (c *Client) ListFiles(repo, path string) ([]string, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}
	_, contents, _, err := c.client.Repositories.GetContents(context.Background(), owner, repoName, path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing %s in %s: %w", path, repo, err)
	}
	var names []string
	for _, item := range contents {
		if item.GetType() == "file" {
			name := item.GetName()
			if strings.HasPrefix(name, "_") {
				continue
			}
			if idx := strings.LastIndex(name, "."); idx > 0 {
				name = name[:idx]
			}
			names = append(names, name)
		}
	}
	return names, nil
}

// ListDirs lists subdirectory names at a path in a GitHub repo. Names starting
// with "_" are skipped — this repo's convention for internal/shared directories
// (_common, _modules, etc.) that aren't real picker entries.
// ref is the branch/tag/SHA to query; empty string uses the repo's default branch.
func (c *Client) ListDirs(repo, path, ref string) ([]string, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}
	var opts *gh.RepositoryContentGetOptions
	if ref != "" {
		opts = &gh.RepositoryContentGetOptions{Ref: ref}
	}
	_, contents, _, err := c.client.Repositories.GetContents(context.Background(), owner, repoName, path, opts)
	if err != nil {
		return nil, fmt.Errorf("listing %s in %s: %w", path, repo, err)
	}
	var names []string
	for _, item := range contents {
		if item.GetType() == "dir" && !strings.HasPrefix(item.GetName(), "_") {
			names = append(names, item.GetName())
		}
	}
	return names, nil
}

// ListTeamMembers returns the GitHub login names of all members of a team.
func (c *Client) ListTeamMembers(org, teamSlug string) ([]string, error) {
	ctx := context.Background()
	var logins []string
	opts := &gh.TeamListTeamMembersOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		members, resp, err := c.client.Teams.ListTeamMembersBySlug(ctx, org, teamSlug, opts)
		if err != nil {
			return nil, fmt.Errorf("listing members of team %s/%s: %w", org, teamSlug, err)
		}
		for _, m := range members {
			logins = append(logins, m.GetLogin())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return logins, nil
}

// ListCollaborators returns the GitHub login names of all collaborators on a repo.
func (c *Client) ListCollaborators(repo string) ([]string, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	var logins []string
	opts := &gh.ListCollaboratorsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		collabs, resp, err := c.client.Repositories.ListCollaborators(ctx, owner, repoName, opts)
		if err != nil {
			return nil, fmt.Errorf("listing collaborators of %s: %w", repo, err)
		}
		for _, col := range collabs {
			logins = append(logins, col.GetLogin())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return logins, nil
}

// FileExists checks whether a file already exists at the given path in the repo.
func (c *Client) FileExists(repo, path string) (bool, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return false, err
	}

	_, _, resp, err := c.client.Repositories.GetContents(context.Background(), owner, repoName, path, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RequestLabel is applied to every pull request platformr opens, so that requests
// can be found later via GitHub search regardless of branch name or title — see
// MyRequestPRs.
const RequestLabel = "platformr"

// CurrentUser returns the login of the authenticated GitHub user.
func (c *Client) CurrentUser() (string, error) {
	user, _, err := c.client.Users.Get(context.Background(), "")
	if err != nil {
		return "", fmt.Errorf("fetching authenticated user: %w", err)
	}
	return user.GetLogin(), nil
}

// PRStatus summarizes a pull request opened by platformr, for the `status` command.
type PRStatus struct {
	Repo      string
	Number    int
	Title     string
	Body      string
	State     string // "open", "merged", or "closed"
	URL       string
	CreatedAt time.Time
}

// MyRequestPRs returns pull requests in repo labeled RequestLabel and authored by
// user, newest first. The label and author filters are applied server-side by
// GitHub search, so only matching PRs are ever fetched — not the whole repo's PR list.
//
// maxResults bounds how many are fetched, stopping as soon as that many are
// collected (results arrive newest-first, so this is a correct "N most recent",
// not an arbitrary truncation) — pass 0 to paginate through every result with no
// limit at all. Without a bound, a caller wanting only the most recent handful
// would otherwise still pay for full pagination across however much history exists.
//
// Filtering by author happens server-side (GitHub search's author: qualifier
// has proven reliable); filtering by label does not — confirmed directly that
// GitHub's label: search filter can keep matching a PR for hours after its
// label was removed, even though the very same search response's own Labels
// field for that PR is already correct. So the label check happens client-side
// against that field instead of trusting the query clause for it.
func (c *Client) MyRequestPRs(repo, user string, maxResults int) ([]PRStatus, error) {
	ctx := context.Background()
	query := fmt.Sprintf("repo:%s is:pr author:%s", repo, user)
	opts := &gh.SearchOptions{
		Sort:        "created",
		Order:       "desc",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var out []PRStatus
	for {
		result, resp, err := c.client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, fmt.Errorf("searching pull requests for %s: %w", repo, err)
		}
		for _, issue := range result.Issues {
			if !hasLabel(issue, RequestLabel) {
				continue
			}
			state := "closed"
			switch issue.GetState() {
			case "open":
				state = "open"
			default:
				if issue.PullRequestLinks != nil && issue.PullRequestLinks.MergedAt != nil {
					state = "merged"
				}
			}
			out = append(out, PRStatus{
				Repo:      repo,
				Number:    issue.GetNumber(),
				Title:     issue.GetTitle(),
				Body:      issue.GetBody(),
				State:     state,
				URL:       issue.GetHTMLURL(),
				CreatedAt: issue.GetCreatedAt().Time,
			})
			if maxResults > 0 && len(out) >= maxResults {
				return out, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// hasLabel reports whether issue currently carries a label named name.
func hasLabel(issue *gh.Issue, name string) bool {
	for _, l := range issue.Labels {
		if l.GetName() == name {
			return true
		}
	}
	return false
}

// ensureRequestLabel creates the RequestLabel label in repo if it doesn't already exist.
func (c *Client) ensureRequestLabel(ctx context.Context, owner, repo string) error {
	_, resp, err := c.client.Issues.CreateLabel(ctx, owner, repo, &gh.Label{
		Name:        gh.String(RequestLabel),
		Color:       gh.String("6f42c1"),
		Description: gh.String("Opened via platformr"),
	})
	if err == nil || (resp != nil && resp.StatusCode == 422) {
		return nil // created, or already exists
	}
	return err
}

// CreatePR creates a branch, commits one or more files, and opens a pull request.
// If req.Files is non-empty the Git tree API is used for an atomic multi-file commit.
// Otherwise req.FilePath + req.Content are committed as a single file.
func (c *Client) CreatePR(req PRRequest) (string, error) {
	ctx := context.Background()

	owner, repo, err := parseRepo(req.Repo)
	if err != nil {
		return "", err
	}

	baseBranch := req.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	if len(req.Files) > 0 {
		return c.createMultiFilePR(ctx, owner, repo, baseBranch, req)
	}
	return c.createSingleFilePR(ctx, owner, repo, baseBranch, req)
}

// createSingleFilePR commits one file via the Contents API and opens a PR.
func (c *Client) createSingleFilePR(ctx context.Context, owner, repo, baseBranch string, req PRRequest) (string, error) {
	baseRef, _, err := c.client.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranch)
	if err != nil {
		return "", fmt.Errorf("getting base ref %q: %w", baseBranch, err)
	}

	newRef := &gh.Reference{
		Ref:    gh.String("refs/heads/" + req.Branch),
		Object: &gh.GitObject{SHA: baseRef.Object.SHA},
	}
	if _, _, err := c.client.Git.CreateRef(ctx, owner, repo, newRef); err != nil {
		return "", fmt.Errorf("creating branch %q: %w", req.Branch, err)
	}

	opts := &gh.RepositoryContentFileOptions{
		Message: gh.String(req.Title),
		Content: []byte(req.Content),
		Branch:  gh.String(req.Branch),
	}
	if _, _, err := c.client.Repositories.CreateFile(ctx, owner, repo, req.FilePath, opts); err != nil {
		return "", fmt.Errorf("creating file %q: %w", req.FilePath, err)
	}

	return c.openPR(ctx, owner, repo, baseBranch, req)
}

// createMultiFilePR commits multiple files via the Git tree API and opens a PR.
func (c *Client) createMultiFilePR(ctx context.Context, owner, repo, baseBranch string, req PRRequest) (string, error) {
	// Resolve base commit + tree
	baseRef, _, err := c.client.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranch)
	if err != nil {
		return "", fmt.Errorf("getting base ref %q: %w", baseBranch, err)
	}
	baseCommitSHA := baseRef.Object.GetSHA()

	baseCommit, _, err := c.client.Git.GetCommit(ctx, owner, repo, baseCommitSHA)
	if err != nil {
		return "", fmt.Errorf("getting base commit: %w", err)
	}

	// Build tree entries — inline content (no need to pre-create blobs)
	entries := make([]*gh.TreeEntry, len(req.Files))
	for i, f := range req.Files {
		entries[i] = &gh.TreeEntry{
			Path:    gh.String(f.Path),
			Mode:    gh.String("100644"),
			Type:    gh.String("blob"),
			Content: gh.String(f.Content),
		}
	}

	tree, _, err := c.client.Git.CreateTree(ctx, owner, repo, baseCommit.Tree.GetSHA(), entries)
	if err != nil {
		return "", fmt.Errorf("creating git tree: %w", err)
	}

	// Create commit
	commit, _, err := c.client.Git.CreateCommit(ctx, owner, repo, &gh.Commit{
		Message: gh.String(req.Title),
		Tree:    tree,
		Parents: []*gh.Commit{{SHA: gh.String(baseCommitSHA)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating commit: %w", err)
	}

	// Create branch pointing at the new commit
	newRef := &gh.Reference{
		Ref:    gh.String("refs/heads/" + req.Branch),
		Object: &gh.GitObject{SHA: commit.SHA},
	}
	if _, _, err := c.client.Git.CreateRef(ctx, owner, repo, newRef); err != nil {
		return "", fmt.Errorf("creating branch %q: %w", req.Branch, err)
	}

	return c.openPR(ctx, owner, repo, baseBranch, req)
}

func (c *Client) openPR(ctx context.Context, owner, repo, baseBranch string, req PRRequest) (string, error) {
	pr, _, err := c.client.PullRequests.Create(ctx, owner, repo, &gh.NewPullRequest{
		Title: gh.String(req.Title),
		Body:  gh.String(req.Body),
		Head:  gh.String(req.Branch),
		Base:  gh.String(baseBranch),
	})
	if err != nil {
		return "", fmt.Errorf("creating pull request: %w", err)
	}

	if err := c.ensureRequestLabel(ctx, owner, repo); err != nil {
		return pr.GetHTMLURL(), fmt.Errorf("PR created but labeling failed: %w", err)
	}
	if _, _, err := c.client.Issues.AddLabelsToIssue(ctx, owner, repo, pr.GetNumber(), []string{RequestLabel}); err != nil {
		return pr.GetHTMLURL(), fmt.Errorf("PR created but labeling failed: %w", err)
	}

	if len(req.Reviewers) > 0 || len(req.TeamReviewers) > 0 {
		_, _, err = c.client.PullRequests.RequestReviewers(ctx, owner, repo, pr.GetNumber(), gh.ReviewersRequest{
			Reviewers:     req.Reviewers,
			TeamReviewers: req.TeamReviewers,
		})
		if err != nil {
			return pr.GetHTMLURL(), fmt.Errorf("PR created but reviewer assignment failed: %w", err)
		}
	}

	return pr.GetHTMLURL(), nil
}

func parseRepo(repo string) (string, string, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo format %q, expected owner/repo", repo)
	}
	return parts[0], parts[1], nil
}
