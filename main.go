package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/nicklaw5/helix"
	"github.com/schollz/progressbar"
	"github.com/sirupsen/logrus"
)

var (
	userName     string
	userID       string
	clientID     string
	outputFolder string
	maxWorkers   int
)

func init() {
	flag.StringVar(&userName, "user", "", "user name you want to download all vods from")
	flag.StringVar(&userID, "userid", "", "user id you want to download all vods from")
	flag.StringVar(&clientID, "clientid", "", "twitch client id to get vods and user information")
	flag.IntVar(&maxWorkers, "workers", 4, "max parallel downloads")
	flag.StringVar(&outputFolder, "output", "", "folder where everything gets saved to")
	flag.Parse()
}

func main() {
	if userName == "" && userID == "" {
		logrus.Warn("missing user or user_id flag")
		return
	}
	if userName != "" && userID != "" {
		logrus.Warn("only use one, user or user_id")
		return
	}

	h, err := createHelixClient(clientID)
	if err != nil {
		logrus.WithError(err).Error("failed creating twitch helix client")
		return
	}

	if userName != "" {
		user, err := getUserID(h, userName)
		if err != nil {
			logrus.WithError(err).Error("failed getting user id")
			return
		}
		userID = user.ID
		userName = user.DisplayName
	}

	if userID != "" {
		user, err := getUserName(h, userID)
		if err != nil {
			logrus.WithError(err).Error("failed getting user id")
			return
		}
		userID = user.ID
		userName = user.DisplayName
	}

	if outputFolder != "" {
		err := os.Chdir(outputFolder)
		if err != nil {
			logrus.WithError(err).Error("failed changing directory")
			return
		}
	}

	CreateDirIfNotExist(userName)

	videos, err := getVideos(h, userID)
	if err != nil {
		logrus.WithError(err).Error("failed creating twitch helix client")
		return
	}
	logrus.Infof("got %d VOD's", len(videos))

	workChan := make(chan helix.Video, len(videos))
	var wg sync.WaitGroup
	wg.Add(len(videos))

	logrus.Infof("starting %d workers", maxWorkers)

	bar := progressbar.NewOptions(len(videos), progressbar.OptionSetRenderBlankState(true))
	// Render the current state, which is 0% in this case
	bar.RenderBlank()

	for i := 0; i < maxWorkers; i++ {
		go func(pb *progressbar.ProgressBar, wg *sync.WaitGroup) {
			for vod := range workChan {
				download(vod, userName)
				wg.Done()
				pb.Add(1)
			}
		}(bar, &wg)
	}

	for _, vod := range videos {
		// download vod
		workChan <- vod
	}
	wg.Wait()
}

// Config ...
type Config struct {
	ClientID string `toml:"client_id"`
}

func createHelixClient(clientID string) (*helix.Client, error) {
	return helix.NewClient(&helix.Options{
		ClientID: clientID,
	})
}

func getUserID(client *helix.Client, user string) (*helix.User, error) {
	response, err := client.GetUsers(&helix.UsersParams{
		Logins: []string{user},
	})
	if err != nil {
		return nil, err
	}
	return &response.Data.Users[0], nil
}

func getUserName(client *helix.Client, userID string) (*helix.User, error) {
	response, err := client.GetUsers(&helix.UsersParams{
		IDs: []string{userID},
	})
	if err != nil {
		return nil, err
	}
	return &response.Data.Users[0], nil
}

func getVideos(client *helix.Client, userID string) ([]helix.Video, error) {
	logrus.Info("getting VOD's...")
	curser := ""
	var videos []helix.Video
	for {
		response, err := client.GetVideos(&helix.VideosParams{
			First:  100,
			After:  curser,
			UserID: userID,
		})
		if err != nil {
			return nil, err
		}

		for _, vod := range response.Data.Videos {
			if !containsVOD(videos, vod.ID) {
				videos = append(videos, vod)
			}
		}

		if response.Data.Pagination.Cursor == "" || response.Data.Pagination.Cursor == curser {
			break
		}
		curser = response.Data.Pagination.Cursor
		time.Sleep(1 * time.Second)
	}
	return videos, nil
}

func containsVOD(vods []helix.Video, id string) bool {
	for _, v := range vods {
		if id == v.ID {
			return true
		}
	}
	return false
}

func download(vod helix.Video, userName string) {
	c, _ := os.Getwd()
	// 2018-03-02T20:53:41Z
	created, _ := time.Parse(time.RFC3339, vod.CreatedAt)
	folder := filepath.Join(c, userName, created.Format("2006-01-02"))
	CreateDirIfNotExist(folder)

	args := []string{
		"--no-part",
		"-c",
		"--write-info-json",
		vod.URL,
	}
	cmd := exec.Command("youtube-dl", args...)
	cmd.Dir = folder
	if out, err := cmd.CombinedOutput(); err != nil {
		logrus.WithError(err).Error("failed downloading vod")
		logrus.Errorf("%s", out)
	}
	info, err := json.MarshalIndent(vod, "", "\t")
	if err != nil {
		return
	}
	ioutil.WriteFile(filepath.Join(userName, created.Format("2006-01-02"), "metadata.json"), info, 0755)
}

// CreateDirIfNotExist ...
func CreateDirIfNotExist(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			panic(err)
		}
	}
}
