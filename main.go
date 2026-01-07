package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
)

type OpenAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	apiUrl := os.Getenv("GEMINI_API_URL")
	model := os.Getenv("GEMINI_MODEL")
	targetLang := os.Getenv("TARGET_LANGUAGE")

	if targetLang == "" {
		targetLang = "German" // My personal Fallback
	}

	if apiKey == "" || apiUrl == "" || model == "" {
		log.Fatal("GEMINI_API_KEY, GEMINI_API_URL, and GEMINI_MODEL must be set")
	}

	if len(os.Args) < 2 {
		log.Fatal("Usage: epub-translator <input.epub>")
	}

	log.Printf("Starting translation with model: %s, target language: %s", model, targetLang)

	inputPath := os.Args[1]
	timestamp := time.Now().Format("20060102-1504")
	inputFilename := filepath.Base(inputPath)
	outputPath := fmt.Sprintf("translated-%s-%s", timestamp, inputFilename)

	err := processEpub(inputPath, outputPath, apiKey, apiUrl, model, targetLang)
	if err != nil {
		log.Fatalf("Error processing epub: %v", err)
	}

	fmt.Printf("Successfully translated EPUB to %s\n", outputPath)
}

func processEpub(inputPath, outputPath, apiKey, apiUrl, model string, targetLang string) error {
	reader, err := zip.OpenReader(inputPath)
	if err != nil {
		return fmt.Errorf("could not open input epub: %w", err)
	}
	defer reader.Close()

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("could not create output file: %w", err)
	}
	defer outputFile.Close()

	writer := zip.NewWriter(outputFile)
	defer writer.Close()

	numberOfXml := 0
	xmlIndex := 0

	for _, file := range reader.File {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if ext == ".xhtml" || ext == ".html" {
			numberOfXml++
		}
	}

	log.Printf("Found %d HTML/XHTML files to translate.", numberOfXml)

	for _, file := range reader.File {
		ext := strings.ToLower(filepath.Ext(file.Name))

		if ext == ".xhtml" || ext == ".html" {
			xmlIndex++
			log.Printf("Translating %s... (%v/%v)", file.Name, xmlIndex, numberOfXml)
		}

		err := processFile(file, writer, apiKey, apiUrl, model, targetLang)

		if err != nil {
			return fmt.Errorf("error processing file %s: %w", file.Name, err)
		}
	}

	return nil
}

func processFile(file *zip.File, writer *zip.Writer, apiKey, apiUrl, model string, targetLang string) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	w, err := writer.Create(file.Name)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(file.Name))
	if ext == ".xhtml" || ext == ".html" {
		return translateHTML(rc, w, apiKey, apiUrl, model, targetLang)
	}

	_, err = io.Copy(w, rc)
	return err
}

func translateHTML(r io.Reader, w io.Writer, apiKey, apiUrl, model string, targetLang string) error {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return err
	}

	// Tags to translate
	selection := doc.Find("p, h1, h2, h3, h4, h5, h6, li, span")
	log.Printf("  -> Found %d translatable nodes", selection.Length())

	selection.Each(func(i int, s *goquery.Selection) {
		// Only translate if there's text and it's not just whitespace
		if strings.TrimSpace(s.Text()) == "" {
			return
		}

		// Use innerHTML to keep nested tags like <em> or <strong>
		inner, err := s.Html()
		if err != nil {
			return
		}

		translated := translateNode(inner, apiKey, apiUrl, model, targetLang)
		s.SetHtml(translated)

		// Add a small delay to avoid hitting rate limits too quickly
		time.Sleep(200 * time.Millisecond)
	})

	htmlStr, err := doc.Html()
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, htmlStr)
	return err
}

func translateNode(htmlContent, key, url, model string, targetLang string) string {
	maxRetries := 5

	// Start delay for retries (will increase exponentially)
	retryDelay := 5 * time.Second

	systemPrompt := fmt.Sprintf("You are a professional translator. Translate to %s. Keep all HTML tags exactly as they are. Output ONLY the translated content.", targetLang)
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": htmlContent},
		},
	}
	body, _ := json.Marshal(payload)

	for i := 0; i <= maxRetries; i++ {
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))

		if err != nil {
			log.Printf("  -> Error creating request: %v", err)
			return htmlContent
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)

		client := &http.Client{}
		resp, err := client.Do(req)

		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			respBody, _ := io.ReadAll(resp.Body)
			var openAIResp OpenAIResponse
			if err := json.Unmarshal(respBody, &openAIResp); err == nil && len(openAIResp.Choices) > 0 {
				return strings.TrimSpace(openAIResp.Choices[0].Message.Content)
			}
		}

		if i < maxRetries {
			statusInfo := "network error"
			if resp != nil {
				statusInfo = fmt.Sprintf("status %d", resp.StatusCode)
				respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				if len(respBody) > 0 {
					statusInfo += " - " + string(respBody)
				}
				resp.Body.Close()
			}

			log.Printf("  -> Translation failed (%s). Retry %d/%d in %v...", statusInfo, i+1, maxRetries, retryDelay)
			time.Sleep(retryDelay)

			if resp != nil && resp.StatusCode == 429 {
				retryDelay *= 3
			} else {
				retryDelay *= 2
			}
			continue
		}
	}

	// Final fallback if all retries failed
	log.Printf("All retries failed for a block. Keeping original text.")

	return htmlContent + " <span style='color: gray; font-size: 0.8em;'>(⚠️ Translation failed)</span>"
}
