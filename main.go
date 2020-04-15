package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"github.com/fatih/color"
	"github.com/gocolly/colly"
	_ "github.com/mattn/go-sqlite3"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"net/http"
	//"time"
)

const DOWNLOAD_DIR = "./Downloaded/"
const USER_LIST = "users.txt"

type SiteFile struct {
	id         int
	name       string
	link       string
	date       int64
	downloaded int
}

var db *sql.DB

func main() {
	createDb()
	parseSite()
	readFile()
}

func createDb() {
	db, _ = sql.Open("sqlite3", "./grandsmeta.db?cache=shared")
	stCreate, _ := db.Prepare("CREATE TABLE IF NOT EXISTS files (" +
		"id INTEGER PRIMARY KEY, " +
		"name TEXT, " +
		"link TEXT, " +
		"date INTEGER, " +
		"downloaded INTEGER)")
	_, _ = stCreate.Exec()
}

func parseSite() {
	stInsert, _ := db.Prepare("INSERT INTO files (name, link, date, downloaded) VALUES (?, ?, ?, 0)")
	stUpdate, _ := db.Prepare("UPDATE files set date = ? WHERE id = ?")

	c := colly.NewCollector(
		colly.MaxDepth(1),
	)

	c.OnHTML("#conference tr", func(e *colly.HTMLElement) {
		td1 := e.DOM.Find("td:nth-child(1) > a")
		link, _ := td1.Attr("href")

		if len(link) > 0 {
			if strings.Contains(link, "folder") {
				timeStart := time.Now()
				_ = e.Request.Visit("https://www.grandsmeta.ru" + link)
				humanLink, _ := url.QueryUnescape(link)
				fmt.Printf("Время парсинга папки: %s %v \n", filepath.Base(humanLink), time.Now().Sub(timeStart))
			} else {
				date := strings.TrimSpace(e.DOM.Find("td:nth-child(4)").Text())
				tm, err := time.Parse("02.01.2006", date)
				if err != nil {
					fmt.Println("Error while parsing date :", err)
				}

				fileName := strings.ToLower(strings.TrimSpace(td1.Text()))
				siteFile, err := getRowByName(fileName)
				if err != nil {
					// Данных нет, значит инсертим
					_, _ = stInsert.Exec(fileName, link, tm.Unix())
				} else {
					// Данные уже есть, значит обновляем дату
					_, _ = stUpdate.Exec(tm.Unix(), siteFile.id)
				}
			}
		}
	})

	_ = c.Visit("https://www.grandsmeta.ru/download?folder=grandsmeta/data")
}

func readFile() {
	setDownloaded, _ := db.Prepare("UPDATE files set downloaded = 1 WHERE id = ?")

	userDirs, _ := os.Open(USER_LIST)
	defer userDirs.Close()

	userDir := bufio.NewScanner(userDirs)
	for userDir.Scan() {
		color.Blue("Пользователь: %s", userDir.Text())

		files, _ := ioutil.ReadDir(userDir.Text() + "Data/")
		for _, file := range files {
			fileFullName := file.Name()
			fileExt := filepath.Ext(fileFullName)
			if fileExt != ".GSD8" {
				continue
			}

			fileBaseName := strings.TrimSuffix(fileFullName, fileExt) // название файла без расширения
			siteFile, err := getRowByName(strings.ToLower(fileBaseName) + ".zip")
			if err != nil {
				color.Red("В базе нет файла: %s\n", strings.ToLower(fileBaseName)+".zip")
			} else {
				if file.ModTime().Unix() < siteFile.date {
					if siteFile.downloaded == 0 {
						color.Green("Download: %s", strings.ToLower(fileBaseName)+".zip")
						err := DownloadFile(DOWNLOAD_DIR+siteFile.name, siteFile.link)
						if err != nil {
							panic(err)
						}
						_, _ = setDownloaded.Exec(siteFile.id)
					}
					color.Yellow("Copied: %s", strings.ToLower(fileBaseName)+".zip")
					_, _ = copyFile(DOWNLOAD_DIR+siteFile.name, userDir.Text()+"Download/"+siteFile.name)
				}
			}
		}
	}

}

func getRowByName(name string) (SiteFile, error) {
	row := db.QueryRow("SELECT * FROM files WHERE name = $1", name)
	siteFile := SiteFile{}
	err := row.Scan(&siteFile.id, &siteFile.name, &siteFile.link, &siteFile.date, &siteFile.downloaded)
	return siteFile, err
}

// WriteCounter : Progress counter
type WriteCounter struct {
	Total uint64
}

// Write : Use as io.Writer for io.TeeReader
func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.PrintProgress()
	return n, nil
}

// PrintProgress : Print progress to console
func (wc WriteCounter) PrintProgress() {
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\r... %s complete", humanize.Bytes(wc.Total))
}

// DownloadFile : Download file
func DownloadFile(filepath string, url string) error {
	// Create filename + tmp
	out, err := os.Create(filepath + ".tmp")
	if err != nil {
		return err
	}

	// Receive data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	// Create & pass progress
	counter := &WriteCounter{}
	_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	if err != nil {
		return err
	}

	// Below closes lock the file so, defer should not use
	_ = resp.Body.Close()
	out.Close()

	fmt.Print("\n")

	// Remove tmp
	err = os.Rename(filepath+".tmp", filepath)
	if err != nil {
		return err
	}

	return nil
}

func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}
