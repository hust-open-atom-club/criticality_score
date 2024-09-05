package gitmetricsync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"

	_ "github.com/lib/pq"
)

type Config struct {
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

type DependentInfo struct {
	DependentCount         int `json:"dependentCount"`
	DirectDependentCount   int `json:"directDependentCount"`
	IndirectDependentCount int `json:"indirectDependentCount"`
}

func Run(configPath string) {
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	db, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.Host, config.Port, config.User, config.Password, config.Database))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	gitLinks := fetchGitLinks(db)
	syncGitMetrics(db, gitLinks)
}

func loadConfig(path string) (Config, error) {
	var config Config
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(bytes, &config)
	return config, err
}

func fetchGitLinks(db *sql.DB) map[string]bool {
	gitLinks := make(map[string]bool)
	githubRegex := regexp.MustCompile(`https?://github\.com/[^\s/]+/[^\s/]+`)
	tables := []string{"debian_packages", "arch_packages"}
	for _, table := range tables {
		rows, err := db.Query(fmt.Sprintf("SELECT git_link FROM %s", table))
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()

		var gitLink sql.NullString // 使用 sql.NullString 代替 string
		for rows.Next() {
			if err := rows.Scan(&gitLink); err != nil {
				log.Fatal(err)
			}
			if gitLink.Valid && githubRegex.MatchString(gitLink.String) { // 检查 gitLink 是否为有效的非NULL值
				gitLinks[gitLink.String] = true
			}
		}
	}
	return gitLinks
}

func syncGitMetrics(db *sql.DB, gitLinks map[string]bool) {
	githubRegex := regexp.MustCompile(`https?://github\.com/([^/\s]+/[^/\s]+)`)

	// 规范化输入链接
	normalizedLinks := make(map[string]bool)
	for link := range gitLinks {
		if matches := githubRegex.FindStringSubmatch(link); len(matches) > 1 {
			normalizedLink := "https://github.com/" + matches[1]
			if normalizedLink != "" { // 确保规范化后的链接非空
				normalizedLinks[normalizedLink] = true
			}
		}
	}

	// 获取数据库中所有git_links的当前状态
	dbLinks := make(map[string]bool)
	query := `SELECT git_link FROM git_metrics`
	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("Failed to fetch git_links from git_metrics: %v", err)
	}
	defer rows.Close()

	var gitLink string
	for rows.Next() {
		if err := rows.Scan(&gitLink); err != nil {
			log.Fatalf("Failed to scan git_link from git_metrics: %v", err)
		}
		dbLinks[gitLink] = true
	}

	// 检查哪些需要删除
	for link := range dbLinks {
		if !normalizedLinks[link] {
			_, err := db.Exec(`DELETE FROM git_metrics WHERE git_link = $1`, link)
			if err != nil {
				log.Printf("Failed to delete git_link %s: %v", link, err)
			}
		}
	}

	// 检查哪些需要添加
	for link := range normalizedLinks {
		if !dbLinks[link] {
			parts := strings.Split(link, "/")
			if len(parts) >= 5 {
				projectType, projectName, version := "github", parts[3], parts[4]
				depCount := queryDepsDev(projectType, projectName, version)
				_, err := db.Exec(`INSERT INTO git_metrics (git_link, depsdev_count) VALUES ($1, $2)`, link, depCount)
				if err != nil {
					log.Printf("Failed to insert git_link %s: %v", link, err)
				}
			}
		}
	}
}

func queryDepsDev(projectType, projectName, version string) int {
	url := fmt.Sprintf("https://api.deps.dev/v3alpha/systems/%s/packages/%s/versions/%s:dependents", projectType, projectName, version)
	resp, err := http.Get(url)
	if err != nil {
		log.Println("Error querying deps.dev:", err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Error: received non-200 response code")
		return 0
	}

	var info DependentInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		log.Println("Error decoding response:", err)
		return 0
	}

	return info.DependentCount
}

func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "" { // 再次确保不处理空字符串
			keys = append(keys, k)
		}
	}
	return keys
}