/*
 * @Author: 7erry
 * @Date: 2024-11-29 16:49:34
 * @LastEditTime: 2024-11-29 18:23:40
 * @Description:
 */
package enumerator

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/HUSTSecLab/criticality_score/pkg/enumerator/config"
	"github.com/HUSTSecLab/criticality_score/pkg/enumerator/internal/api"
	"github.com/HUSTSecLab/criticality_score/pkg/enumerator/internal/api/bitbucket"
	"github.com/HUSTSecLab/criticality_score/pkg/enumerator/internal/api/gitlab"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/imroc/req/v3"
	"github.com/sirupsen/logrus"
)

type Enumerator struct {
	Client *req.Client
	Token  string
}

func NewEnumerator() *Enumerator {
	return &Enumerator{
		Client: req.C().ImpersonateChrome().SetTimeout(10 * time.Second),
	}
}

func (c *Enumerator) SetToken(token string) {
	c.Token = token
	c.Client.SetCommonBearerAuthToken(token)
}

func (c *Enumerator) Enumerate() {
	var wg sync.WaitGroup
	wg.Add(3)
	gopool.Go(func() {
		defer wg.Done()
		c.enumerateBitbucket()
	})
	gopool.Go(func() {
		defer wg.Done()
		c.enumerateGitlab()
	})
	gopool.Go(func() {
		defer wg.Done()
		c.enumerateCargo()
	})
	wg.Wait()
}

func (c *Enumerator) enumerateBitbucket() {
	u := api.BITBUCKET_ENUMERATE_API_URL
	for {
		resp, err := bitbucket.Fetch(c.Client, u)
		if err != nil {
			logrus.Panic("Bitbucket", err)
		}

		data, err := json.Marshal(resp.Values)
		if err != nil {
			logrus.Panic("Bitbucket", err)
		}

		err = os.WriteFile(config.OUTPUT_DIR+config.BITBUCKET_OUTPUT_FILEPATH, data, 0644)
		if err != nil {
			logrus.Panic("Bitbucket", err)
		}

		u = resp.Next
	}
}

// Todo Use channel to receive and write data
func (c *Enumerator) enumerateGitlab() {
	api_url := api.GITLAB_ENUMERATE_API_URL
	var wg sync.WaitGroup
	wg.Add(1)
	for page := 1; page <= 1; page++ {
		time.Sleep(2 * time.Second)
		gopool.Go(func() {
			defer wg.Done()
			u := fmt.Sprintf(
				"%s?%s=%s&%s=%s&%s=%d&%s=%d",
				api_url,
				"order_by", "star_count",
				"sort", "desc",
				"per_page", api.PER_PAGE,
				"page", page,
			)
			resp, err := gitlab.Fetch(c.Client, u)
			if err != nil {
				logrus.Panic("Gitlab", err)
			}

			data, err := json.Marshal(resp)
			if err != nil {
				logrus.Panic("Gitlab", err)
			}

			err = os.WriteFile(config.OUTPUT_DIR+config.GITLAB_OUTPUT_FILEPATH, data, 0644)
			if err != nil {
				logrus.Panic("Gitlab", err)
			}
		})
	}
	wg.Wait()
}

// ToDo
func (c *Enumerator) enumerateCargo() {

}

// ToDo
func (c *Enumerator) enumeratePyPI() {

}

// ToDo
func (c *Enumerator) enumerateNPM() {

}

// ToDo
func (c *Enumerator) enumerateGo() {

}

// ToDo
func (c *Enumerator) enumeratePHP() {

}

// ToDo
func (c *Enumerator) enumerateHaskell() {

}

// ToDo
func (c *Enumerator) enumerateRubyGems() {

}