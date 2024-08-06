package githubmetrics

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/go-github/v41/github"
	"golang.org/x/oauth2"
)

type Config struct {
	Database    string `json:"database"`
	User        string `json:"user"`
	Password    string `json:"password"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	GitHubToken string `json:"githubToken"`
}

type GitHubStats struct {
	StarCount        int
	ForkCount        int
	CreatedSince     time.Time
	UpdatedSince     time.Time
	ContributorCount int
	CommitFrequency  int
}

func Run(ctx context.Context, db *sql.DB, owner string, repo string, config Config) error {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.GitHubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	stats := GitHubStats{}

	var err error
	var repoInfo *github.Repository
	var contributors []*github.Contributor
	var commits []*github.RepositoryCommit

	// 尝试获取仓库信息
	repoInfo, _, err = client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		wait := handleRateLimitError(err)
		if wait > 0 {
			time.Sleep(wait)
			repoInfo, _, err = client.Repositories.Get(ctx, owner, repo)
		}
	}
	if err == nil {
		stats.StarCount = *repoInfo.StargazersCount
		stats.ForkCount = *repoInfo.ForksCount
		stats.CreatedSince = repoInfo.CreatedAt.Time
		stats.UpdatedSince = repoInfo.UpdatedAt.Time
	} else {
		fmt.Printf("Final error fetching repository info for %s/%s: %v\n", owner, repo, err)
	}

	// 获取贡献者
	contributors, _, err = client.Repositories.ListContributors(ctx, owner, repo, nil)
	if err != nil {
		wait := handleRateLimitError(err)
		if wait > 0 {
			time.Sleep(wait)
			contributors, _, err = client.Repositories.ListContributors(ctx, owner, repo, nil)
		}
	}
	if err == nil {
		stats.ContributorCount = len(contributors)
	} else {
		fmt.Printf("Final error fetching contributors for %s/%s: %v\n", owner, repo, err)
	}

	// 获取提交信息
	now := time.Now()
	aYearAgo := now.AddDate(-1, 0, 0)
	commits, _, err = client.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
		Since: aYearAgo,
		Until: now,
	})
	if err != nil {
		wait := handleRateLimitError(err)
		if wait > 0 {
			time.Sleep(wait)
			commits, _, err = client.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
				Since: aYearAgo,
				Until: now,
			})
		}
	}
	if err == nil {
		stats.CommitFrequency = len(commits) / 52 // Assume 52 weeks in a year
	} else {
		fmt.Printf("Final error fetching commits for %s/%s: %v\n", owner, repo, err)
	}

	err = updateDatabase(ctx, db, owner, repo, stats)
	if err != nil {
		return fmt.Errorf("error updating database for %s/%s: %v", owner, repo, err)
	}
	return nil
}

func handleRateLimitError(err error) time.Duration {
	if rateLimitError, ok := err.(*github.RateLimitError); ok {
		resetTimestamp := rateLimitError.Rate.Reset.Time
		waitDuration := time.Until(resetTimestamp)
		fmt.Printf("GitHub API rate limit exceeded. Waiting %v before retrying...\n", waitDuration)
		return waitDuration
	}
	return 0
}

func updateDatabase(ctx context.Context, db *sql.DB, owner, repo string, stats GitHubStats) error {
	_, err := db.Exec(`UPDATE git_metrics SET star_count = $1, fork_count = $2, created_since = $3, updated_since = $4, contributor_count = $5, commit_frequency = $6 WHERE git_link = $7`,
		stats.StarCount, stats.ForkCount, stats.CreatedSince, stats.UpdatedSince, stats.ContributorCount, stats.CommitFrequency, fmt.Sprintf("https://github.com/%s/%s", owner, repo))
	return err
}
