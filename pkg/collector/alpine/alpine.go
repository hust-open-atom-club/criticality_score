package alpine

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/lib/pq"

	"github.com/HUSTSecLab/criticality_score/pkg/storage"
)

type PackageInfo struct {
	DependsCount int
	Description  string
	Homepage     string
	PageRank     float64
	Version      string
	PackageName  string
	Depends      []string
}

func (pkg *PackageInfo) GetAllDependencies(packages map[string]PackageInfo, visited map[string]bool, deps []string) []string {
	if visited[pkg.PackageName] {
		return deps
	}

	visited[pkg.PackageName] = true
	deps = append(deps, pkg.PackageName)

	for _, depName := range pkg.Depends {
		if depPkg, ok := packages[depName]; ok {
			deps = depPkg.GetAllDependencies(packages, visited, deps)
		}
	}
	return deps
}

type AlpineCollector struct {
	URL      string
	Archlist []string
}

func NewAlpineCollector() *AlpineCollector {
	return &AlpineCollector{
		URL:      "https://mirrors.aliyun.com/alpine/v3.21/main/%s/APKINDEX.tar.gz",
		Archlist: []string{"x86_64"},
	}
}

func (ac *AlpineCollector) Collect(outputPath string) {
	pkgInfoMap, err := ac.getPackageList()
	if err != nil {
		log.Fatal(err)
	}
	depMap := make(map[string][]string)
	for pkgName, pkgInfo := range pkgInfoMap {
		visited := make(map[string]bool)
		deps := pkgInfo.GetAllDependencies(pkgInfoMap, visited, []string{})
		depMap[pkgName] = deps
	}

	countMap := make(map[string]int)
	for _, deps := range depMap {
		for _, dep := range deps {
			countMap[dep]++
		}
	}

	pagerank := ac.calculatePageRank(pkgInfoMap, 20, 0.85)

	for pkgName, pkgInfo := range pkgInfoMap {
		pagerankVal := pagerank[pkgName]
		depCount := countMap[pkgName]
		pkgInfo.PageRank = pagerankVal
		pkgInfo.DependsCount = depCount
		pkgInfoMap[pkgName] = pkgInfo
	}
	err = ac.updateOrInsertDatabase(pkgInfoMap)
	if err != nil {
		fmt.Printf("Error updating database: %v\n", err)
		return
	}
	for pkgName, pkgInfo := range pkgInfoMap {
		if err := ac.storeDependenciesInDatabase(pkgName, pkgInfo.Depends); err != nil {
			if ac.isUniqueViolation(err) {
				continue
			}
			fmt.Printf("Error storing dependencies for package %s: %v\n", pkgName, err)
		}
	}
	fmt.Println("Database updated successfully.")

	if outputPath != "" {
		err := ac.generateDependencyGraph(pkgInfoMap, outputPath)
		if err != nil {
			fmt.Printf("Error generating dependency graph: %v\n", err)
			return
		}
		fmt.Println("Dependency graph generated successfully.")
	}
}

func (ac *AlpineCollector) getPackageList() (map[string]PackageInfo, error) {
	var data string
	for _, arch := range ac.Archlist {
		url := fmt.Sprintf(ac.URL, arch)
		resp, err := http.Get(url)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		res, err := ac.decompressGzip(body)
		if err != nil {
			log.Fatal(err)
		}
		data += res
	}
	packageList := make(map[string]PackageInfo)
	entries := strings.Split(data, "\n\n")
	for _, entry := range entries {
		lines := strings.Split(entry, "\n")
		var pkg PackageInfo
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
			switch line[0:2] {
			case "P:":
				pkg.PackageName = line[2:]
			case "V:":
				pkg.Version = line[2:]
			case "D:":
				depends := strings.Fields(line[2:])
				for _, dep := range depends {
					if idx := strings.Index(dep, ":"); idx != -1 {
						dep = dep[idx+1:]
					}
					if idx := strings.Index(dep, "="); idx != -1 {
						dep = dep[:idx]
					}
					pkg.Depends = append(pkg.Depends, dep)
				}
			case "T:":
				pkg.Description = line[2:]
			case "U:":
				pkg.Homepage = line[2:]
			}
		}
		if pkg.PackageName != "" {
			packageList[pkg.PackageName] = pkg
		}
	}
	return packageList, nil
}

func (ac *AlpineCollector) decompressGzip(data []byte) (string, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer reader.Close()

	uncompressedData, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(uncompressedData), nil
}

func (ac *AlpineCollector) calculatePageRank(pkgInfoMap map[string]PackageInfo, iterations int, dampingFactor float64) map[string]float64 {
	pageRank := make(map[string]float64)
	numPackages := len(pkgInfoMap)

	for pkgName := range pkgInfoMap {
		pageRank[pkgName] = 1.0 / float64(numPackages)
	}

	for i := 0; i < iterations; i++ {
		newPageRank := make(map[string]float64)

		for pkgName := range pkgInfoMap {
			newPageRank[pkgName] = (1 - dampingFactor) / float64(numPackages)
		}

		for pkgName, pkgInfo := range pkgInfoMap {
			var depNum int
			for _, depName := range pkgInfo.Depends {
				if _, exists := pkgInfoMap[depName]; exists {
					depNum++
				}
			}
			for _, depName := range pkgInfo.Depends {
				if _, exists := pkgInfoMap[depName]; exists {
					newPageRank[depName] += dampingFactor * (pageRank[pkgName] / float64(depNum))
				}
			}
		}
		pageRank = newPageRank
	}
	return pageRank
}

func (ac *AlpineCollector) isUniqueViolation(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "23505"
	}
	return false
}

func (ac *AlpineCollector) updateOrInsertDatabase(pkgInfoMap map[string]PackageInfo) error {
	db, err := storage.GetDefaultAppDatabaseContext().GetDatabaseConnection()
	if err != nil {
		return err
	}
	defer db.Close()

	for pkgName, pkgInfo := range pkgInfoMap {
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM alpine_packages WHERE package = $1)", pkgName).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			_, err := db.Exec("INSERT INTO alpine_packages (package, depends_count, description, homepage, page_rank, version) VALUES ($1, $2, $3, $4, $5, $6)",
				pkgName, pkgInfo.DependsCount, pkgInfo.Description, pkgInfo.Homepage, pkgInfo.PageRank, pkgInfo.Version)
			if err != nil {
				return err
			}
		} else {
			_, err := db.Exec("UPDATE alpine_packages SET depends_count = $1, description = $2, homepage = $3, page_rank = $4, version = $5 WHERE package = $6",
				pkgInfo.DependsCount, pkgInfo.Description, pkgInfo.Homepage, pkgInfo.PageRank, pkgInfo.Version, pkgName)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ac *AlpineCollector) storeDependenciesInDatabase(pkgName string, dependencies []string) error {
	db, err := storage.GetDefaultAppDatabaseContext().GetDatabaseConnection()
	if err != nil {
		return err
	}
	defer db.Close()

	for _, dep := range dependencies {
		_, err := db.Exec("INSERT INTO alpine_relationships (frompackage, topackage) VALUES ($1, $2)", pkgName, dep)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ac *AlpineCollector) generateDependencyGraph(pkgInfoMap map[string]PackageInfo, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writer.WriteString("digraph {\n")

	packageIndices := make(map[string]int)
	index := 0

	for pkgName, pkgInfo := range pkgInfoMap {
		packageIndices[pkgName] = index
		label := fmt.Sprintf("%s@%s", pkgName, pkgInfo.Description)
		writer.WriteString(fmt.Sprintf("  %d [label=\"%s\"];\n", index, label))
		index++
	}

	for pkgName, pkgInfo := range pkgInfoMap {
		pkgIndex := packageIndices[pkgName]
		for _, depName := range pkgInfo.Depends {
			if depIndex, ok := packageIndices[depName]; ok {
				writer.WriteString(fmt.Sprintf("  %d -> %d;\n", pkgIndex, depIndex))
			}
		}
	}

	writer.WriteString("}\n")
	writer.Flush()
	return nil
}
