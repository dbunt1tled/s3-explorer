package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"s3explorer/internal/config/env"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	var (
		selectFile string
		bucketID   widget.ListItemID
		bucketName string
		files      []string
	)
	config := env.GetConfigInstance()
	cfg := aws.Config{
		Region: config.S3.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			config.S3.AccessKey,
			config.S3.SecretKey,
			"", // token empty for not temporary credentials
		),
	}
	s3Client := s3.NewFromConfig(cfg)
	myApp := app.New()
	myWindow := myApp.NewWindow("S3 Explorer")
	myWindow.SetMaster()
	buckets := getBuckets(s3Client)
	bucketList := widget.NewList(
		func() int {
			return len(buckets)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("template")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(buckets[i])
		})
	title := widget.NewLabel("Folders/Files")
	title.Alignment = fyne.TextAlignCenter
	title.TextStyle = fyne.TextStyle{Bold: true}
	fileList := widget.NewList(
		func() int { return 0 },
		func() fyne.CanvasObject {
			return widget.NewLabel("File/Folder")
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {},
	)
	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	fileScroll := container.NewVScroll(fileList)
	fileScroll.SetMinSize(fyne.NewSize(200, 300)) //nolint:mnd // Base size

	bucketList.OnSelected = func(id widget.ListItemID) {
		progressBar.Hide()
		bucketName = buckets[id]
		bucketID = id
		files = getBucketContents(s3Client, bucketName)
		fileList.Length = func() int { return len(files) }
		fileList.UpdateItem = func(id widget.ListItemID, item fyne.CanvasObject) {
			item.(*widget.Label).SetText(files[id])
		}
		fileList.Refresh()
	}
	fileList.OnSelected = func(id widget.ListItemID) {
		selectFile = files[id]
	}

	uploadButton := widget.NewButton("Upload", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			filePath := reader.URI().Path()
			reader.Close() // Close the file, since it was opened by the dialog

			uploadFile(s3Client, bucketName, filePath, progressBar, myWindow)
		}, myWindow)
		bucketList.OnSelected(bucketID)
	})
	downloadButton := widget.NewButton("Download", func() {
		if fileList.Length() == 0 || selectFile == "" || selectFile[len(selectFile)-1:] == "/" {
			dialog.ShowInformation("Error", "Choose a file to download", myWindow)
			return
		}

		selectedFile := selectFile
		saveDialog := dialog.NewFileSave(func(savePath fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(fmt.Errorf("error selecting path: %v", err), myWindow)
				progressBar.Hide()
				return
			}
			if savePath == nil {
				return // User canceled the selection
			}

			downloadFile(s3Client, bucketName, selectedFile, savePath.URI().Path(), progressBar, myWindow)
		}, myWindow)
		saveDialog.SetFileName(filepath.Base(selectedFile))
		saveDialog.Show()
	})
	deleteButton := widget.NewButton("Delete", func() {
		progressBar.Hide()
		if fileList.Length() == 0 || selectFile == "" || selectFile[len(selectFile)-1:] == "/" {
			dialog.ShowInformation("Error", "Choose a file to delete", myWindow)
			return
		}
		fileKey := filepath.Base(selectFile)
		d := dialog.NewConfirm("Delete", fmt.Sprintf("Delete File %s?", fileKey), func(confirmed bool) {
			if confirmed {
				err := deleteFile(s3Client, bucketName, selectFile)
				if err != nil {
					dialog.ShowError(fmt.Errorf("error deleting file: %v", err), myWindow)
					progressBar.Hide()
					return
				} else {
					log.Printf("File %s deleted successfully", fileKey)
				}
				bucketList.OnSelected(bucketID)
			}
		}, myWindow)
		d.Show()
	})

	buttons := container.NewHBox(uploadButton, downloadButton, deleteButton)

	rightPane := container.NewBorder(
		title, // Top
		container.NewVBox(
			buttons,
			progressBar,
		), // Нижняя часть
		nil,        // Left (not used)
		nil,        // Right (not used)
		fileScroll, // Center
	)

	split := container.NewHSplit(bucketList, rightPane)
	split.Offset = 0.3
	myWindow.SetContent(split)
	myWindow.ShowAndRun()
}

func getBuckets(client *s3.Client) []string {
	resp, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	if err != nil {
		log.Printf("unable to list buckets, %v", err)
		return nil
	}

	var buckets []string
	for _, b := range resp.Buckets {
		buckets = append(buckets, aws.ToString(b.Name))
	}
	return buckets
}

func getBucketContents(client *s3.Client, bucketName string) []string {
	resp, err := client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		log.Printf("unable to list objects for bucket %s, %v", bucketName, err)
		return nil
	}

	var contents []string
	for _, item := range resp.Contents {
		contents = append(contents, aws.ToString(item.Key))
	}
	return contents
}

func downloadFile(
	client *s3.Client,
	bucketName, fileName, savePath string,
	progressBar *widget.ProgressBar,
	window fyne.Window,
) {
	log.Printf("Downloading file %s from bucket %s to %s", fileName, bucketName, savePath)

	resp, err := client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
	})
	if err != nil {
		dialog.ShowError(fmt.Errorf("error getting object: %v", err), window)
		return
	}
	defer resp.Body.Close()

	// Создание файла
	outFile, err := os.Create(savePath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("error creating file: %v", err), window)
		return
	}
	defer outFile.Close()

	// Set the progress bar
	size := resp.ContentLength
	buffer := make([]byte, 1024*1024) // 1 MB
	var downloaded int64
	progressBar.SetValue(0)
	progressBar.Show()
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := outFile.Write(buffer[:n]); writeErr != nil {
				dialog.ShowError(fmt.Errorf("error writing file: %v", writeErr), window)
				return
			}
			downloaded += int64(n)
			progressBar.SetValue(float64(downloaded) / float64(*size))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			dialog.ShowError(fmt.Errorf("error reading from S3: %v", err), window)
			return
		}
	}

	dialog.ShowInformation("Success", "File successfully downloaded", window)
}

func uploadFile(client *s3.Client, bucketName, path string, progressBar *widget.ProgressBar, window fyne.Window) {
	file, err := os.Open(path)
	if err != nil {
		dialog.ShowError(fmt.Errorf("error opening file: %v", err), window)
		return
	}
	defer file.Close()
	// Get the file information
	fileInfo, err := file.Stat()
	if err != nil {
		dialog.ShowError(fmt.Errorf("error getting file information: %v", err), window)
		return
	}
	progressBar.SetValue(0)
	progressBar.Show()

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileInfo.Name()),
		Body:   file,
	})
	if err != nil {
		dialog.ShowError(fmt.Errorf("error uploading file: %v", err), window)
	} else {
		progressBar.SetValue(1)
		dialog.ShowInformation("Success", "File successfully uploaded", window)
	}
}

func deleteFile(client *s3.Client, bucketName string, fileName string) error {
	_, err := client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
	})
	return err
}
