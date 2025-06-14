package utils

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/melbahja/got"
	"github.com/xxjwxc/gowp/workpool"
	"go.uber.org/zap"
	"golang.org/x/text/unicode/norm"

	"asmr-downloader/log"
)

const FailedDownloadFileName = "failed-download.txt"

var FailedDownloadFile *os.File

func init() {
	f, err := os.OpenFile(FailedDownloadFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.AsmrLog.Error("错误日志文件创建失败: ", zap.String("error", err.Error()))
	}
	FailedDownloadFile = f
}

// Client httpClient
var Client = sync.Pool{
	New: func() interface{} {
		return &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					//update tls version,version 12 may cause error on cf worker
					MaxVersion: tls.VersionTLS13,
				},
			},
		}
	},
}

// FileOrDirExists  判断所给路径文件/文件夹是否存在 after unicode normalization
func FileOrDirExists(path string) bool {
	path = norm.NFC.String(path)
	//path = strings.ReplaceAll(path, "/jfs/", "/ASMR/")
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}

	for _, file := range files {
		if norm.NFC.String(file.Name()) == norm.NFC.String(filepath.Base(path)) {
			return true
		}
	}
	return false

}

// PromotForInput 获取用户输入
func PromotForInput(message string, defaultValue string) string {
	log.AsmrLog.Info(message)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	value := scanner.Text()
	err := scanner.Err()
	if err != nil {
		log.AsmrLog.Info(fmt.Sprintf("输入有误: %s", value))
		os.Exit(0)
	}
	if value == "" {
		return defaultValue
	}
	all := strings.ReplaceAll(value, "\n", "")
	return strings.TrimSpace(all)
}

// NewWorkerPool 工作池
func NewWorkerPool(maxWorkerCount int) *workpool.WorkPool {
	return workpool.New(maxWorkerCount)
}

// MosaicStr 模糊字符串
func MosaicStr(inputStr string, mosaicStrTmp string) string {
	if mosaicStrTmp == "" {
		mosaicStrTmp = "*"
	}
	var result = strings.Builder{}
	size := len(inputStr)
	for i := 0; i < size; i++ {
		result.WriteString(mosaicStrTmp)
	}
	return result.String()
}

// GenerateReqSeed 生成请求种子 seed参数
func GenerateReqSeed() int {
	rand.Seed(time.Now().UnixNano())
	result := int(100 * rand.Float64())
	return result
}

// CalculateMaxPage
//
//	@Description: 计算最大页数
//	@param totalCount 总数据
//	@param pageSize 每页数据
//	@return int 最大页数
func CalculateMaxPage(totalCount int, pageSize int) int {
	if totalCount < 0 || pageSize <= 0 {
		panic("totalCount必须大于等于0, pageSize必须大于0")
	}
	if totalCount == 0 {
		return 1
	}
	i := totalCount / pageSize
	padding := totalCount % pageSize
	if padding != 0 {
		i += 1
	}
	return i
}

func DownloadFile(storePath string, fileUrl string) error {
	client := &http.Client{}

	req, err := http.NewRequest("GET", fileUrl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(storePath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// NewFileDownloader
//
//	@Description: 下载文件
//	@param url
//	@param path
//	@param filename
//	@return func()
func NewFileDownloader(url string, path string, filename string) func() error {
	return func() error {
		var fileUrl = url
		var filePathToStore = path
		var fileName = filename
		var storePath = filepath.Join(filePathToStore, fileName)
		fileClient := got.New()
		err := fileClient.Download(fileUrl, storePath)

		if err != nil {
			// Retry with http.Get
			if strings.Contains(err.Error(), "Content-Length") {
				err = DownloadFile(storePath, fileUrl)
			}
			if err == nil {
				log.AsmrLog.Info("文件下载成功: ", zap.String("info", fileName))
				return nil
			}

			log.AsmrLog.Error(err.Error())
			//fmt.Printf("文件: %s下载失败: %s\n", fileName, fileUrl)
			log.AsmrLog.Error(fmt.Sprintf("文件: %s下载失败: %s", fileName, err.Error()))

			if err := log.DiscordWebhook.Send(fmt.Sprintf("文件: %s下载失败: %s", storePath, err.Error())); err != nil {
				log.AsmrLog.Error("发送Discord Webhook失败: ", zap.String("error", err.Error()))
			}

			//记录失败文件  时间, 文件路径，文件url
			logStr := GetCurrentDateTime() + "|" + storePath + "|" + fileUrl + "\n"
			write := bufio.NewWriter(FailedDownloadFile)
			_, _ = write.WriteString(logStr)
			//Flush将缓存的文件真正写入到文件中
			write.Flush()
			//清理下载失败的文件碎片
			err2 := os.Remove(storePath)
			if err2 != nil {
				log.AsmrLog.Error("删除碎片文件失败文件失败:", zap.String("error", err2.Error()))
			}
		} else {
			log.AsmrLog.Info("文件下载成功: ", zap.String("info", fileName))
			//fmt.Println("文件下载成功: ", filePathToStore)
		}
		return nil
	}

}

// GetCurrentDateTime
//
//	@Description: 获取当前时间
//	@return string
func GetCurrentDateTime() string {
	now := time.Now()
	// Format the time using the standard format string
	currentTimeStr := now.Format("2006-01-02 15:04:05")
	return currentTimeStr
}

// NewFixFileDownloader
//
//	 下载上一次循环下载出错的文件
//		@Description: 下载
//		@param url
//		@param storePath
//		@param resultLines
//		@return []string
//		@return error
func NewFixFileDownloader(url string, storePath string, resultLines []string) ([]string, error) {
	//确保路径存在
	exists := FileOrDirExists(storePath)
	if !exists {
		dir := filepath.Dir(storePath)
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			log.AsmrLog.Error(fmt.Sprintf("自动创建上一次下载失败文件目录失败: %s", err))
			return nil, nil
		}
	}
	// Remove the file if there exists 1015 error
	content, err := os.ReadFile(storePath)
	if err == nil && string(content) == "error code: 1015" {
		_ = os.Remove(storePath)

		// Don't download again if file exists
	} else if err == nil {
		return resultLines, nil
	}

	err = DownloadFile(storePath, url)
	if err != nil {
		log.AsmrLog.Error(err.Error())
		//fmt.Printf("文件: %s下载失败: %s\n", fileName, url)
		log.AsmrLog.Error(fmt.Sprintf("文件: %s下载失败: %s", storePath, err.Error()))

		if err := log.DiscordWebhook.Send(fmt.Sprintf("文件: %s下载失败: %s", storePath, err.Error())); err != nil {
			log.AsmrLog.Error("发送Discord Webhook失败: ", zap.String("error", err.Error()))
		}
		//记录失败文件  时间, 文件路径，文件url
		logStr := GetCurrentDateTime() + "|" + storePath + "|" + url
		resultLines = append(resultLines, logStr)
	} else {
		// Handle cloudflare 1015 error
		content, err := os.ReadFile(storePath)
		if err == nil && string(content) == "error code: 1015" {
			log.AsmrLog.Error(fmt.Sprintf("文件: %s 下载遇到了 1015 错误，休眠10秒后重试。", storePath))
			if err := log.DiscordWebhook.Send(fmt.Sprintf("文件: %s 下载遇到了 1015 错误，休眠10秒后重试。", storePath)); err != nil {
				log.AsmrLog.Error("发送Discord Webhook失败: ", zap.String("error", err.Error()))
			}
			time.Sleep(time.Second * 10)
			resultLines = append(resultLines, GetCurrentDateTime()+"|"+storePath+"|"+url)
			return resultLines, nil
		}

		log.AsmrLog.Info("文件下载成功: ", zap.String("info", storePath))
	}
	return resultLines, nil
}

// FixBrokenDownloadFile
//
//	@Description: 以最大重试方式修复下载出错的文件
//	@param maxRetry
func FixBrokenDownloadFile(maxRetry int) {
	log.AsmrLog.Info("正在自动处理下载失败的媒体文件,请稍后...")
	//复制下载出错的日志文件
	var FailedDownloadFileNameTemp = FailedDownloadFileName + ".tmp"
	err := CopyFile(FailedDownloadFileName, FailedDownloadFileName+".tmp")
	if err != nil {
		log.AsmrLog.Error(fmt.Sprintf("复制文件: %s失败: %s", FailedDownloadFileName, err.Error()))
		return
	}
	fi, err := os.Open(FailedDownloadFileNameTemp)
	if err != nil {
		log.AsmrLog.Error(fmt.Sprintf("Error: %s", err))
		return
	}

	br := bufio.NewReader(fi)
	var resultLine = []string{}
	for {
		line, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		if len(strings.Trim(string(line), "\r\n")) > 0 {
			resultLine = append(resultLine, string(line))
		}
	}
	fi.Close()
	var resultContainer = []string{}
	var lastSuccessIndex = -1
	for index, brokenLine := range resultLine {
		for i := 0; i < maxRetry; i++ {
			if index == lastSuccessIndex {
				break
			}
			log.AsmrLog.Info(fmt.Sprintf("index: %d,line: %s", index, brokenLine))
			fileInfos := strings.Split(brokenLine, "|")
			downloader, _ := NewFixFileDownloader(fileInfos[2], fileInfos[1], resultContainer)
			resultContainer = downloader
			if len(resultContainer) <= 0 {
				lastSuccessIndex = index
				break
			}
			if err := log.DiscordWebhook.Send(fmt.Sprintf("重试下载文件再次出错,重试中(剩余重试次数: %d)...", maxRetry-i-1)); err != nil {
				log.AsmrLog.Error("发送Discord Webhook失败: ", zap.String("error", err.Error()))
			}
			log.AsmrLog.Info(fmt.Sprintf("重试下载文件再次出错,重试中(剩余重试次数: %d)...", maxRetry-i-1))
		}
	}
	//删除temp文件
	err2 := os.Remove(FailedDownloadFileNameTemp)
	if err2 != nil {
		log.AsmrLog.Error("删除临时文件失败:", zap.String("error", err2.Error()))
		return
	}
	//清理文件
	err = FailedDownloadFile.Truncate(0)
	if err != nil {
		log.AsmrLog.Error("清空下载失败日志文件失败:", zap.String("error", err.Error()))
		return
	}
	log.AsmrLog.Info("重试下载失败媒体文件已处理完成!")

}

// CheckIfNeedFixBrokenDownloadFile
// CheckIfNeedFixBroken
//
//	@Description: 检测是否需要修复下载出错的文件
//	@return bool
func CheckIfNeedFixBrokenDownloadFile() bool {
	file, err := os.OpenFile(FailedDownloadFileName, os.O_RDONLY, 0666)
	defer file.Close()
	if err != nil {
		log.AsmrLog.Error(fmt.Sprintf("打开文件失败: %s", err.Error()))
		return false
	}
	br := bufio.NewReader(file)
	var resultLine = []string{}
	for {
		line, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		if len(strings.Trim(string(line), "\r\n")) > 0 {
			resultLine = append(resultLine, string(line))
		}
	}
	return len(resultLine) != 0
}

// CopyFile
//
//	@Description: 复制文件
//	@param src
//	@param dst
//	@return err
func CopyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if e := out.Close(); e != nil {
			err = e
		}
	}()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	err = out.Sync()
	if err != nil {
		return err
	}

	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	err = os.Chmod(dst, si.Mode())
	if err != nil {
		return err
	}
	return err
}

func FastFetch(url string, wg *sync.WaitGroup, ch chan<- string) {
	defer wg.Done()

	startTime := time.Now()
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error fetching %s: %v\n", url, err)
		return
	}
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response body from %s: %v\n", url, err)
		return
	}

	duration := time.Since(startTime)
	ch <- fmt.Sprintf("%s|%s", url, duration)
}
