package main

import (
	"fmt"
	"os"

	url "github.com/HUSTSecLab/criticality_score/pkg/gitfile/parser/url"
	"github.com/HUSTSecLab/criticality_score/pkg/gitfile/util"
	"github.com/HUSTSecLab/criticality_score/pkg/logger"
	"github.com/spf13/pflag"

	"github.com/go-git/go-git/v5"
)

var flagStoragePath = pflag.StringP("storage", "s", "./storage", "path to git storage location")

func main() {
	var path string
	if len(os.Args) == 2 {
		path = os.Args[1]
	} else {
		path = ""
	}
	inputs, err := util.GetCSVInput(path)
	if err != nil {
		logger.Fatalf("Reading %s Failed", path)
	}
	var count int
	for _, input := range inputs {
		u := url.ParseURL(input[0])
		path = *flagStoragePath + u.Pathname
		_, err := git.PlainOpen(path)
		if err == git.ErrRepositoryNotExists {
			logger.Infof("%s Not Collected", input[0])
			continue
		}
		count++
	}
	fmt.Println(count)
}