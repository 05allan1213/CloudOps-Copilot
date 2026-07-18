package runbook

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func ParseMarkdown(file string, data []byte) (Document, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	doc := Document{
		ID:        strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
		File:      filepath.Base(file),
		Body:      strings.TrimSpace(string(data)),
		UpdatedAt: time.Now().UTC(),
	}

	var current *Section
	var listTarget *[]string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "# ") && doc.Title == "":
			doc.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			listTarget = nil
		case strings.HasPrefix(line, "## "):
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			doc.Sections = append(doc.Sections, Section{Heading: heading})
			current = &doc.Sections[len(doc.Sections)-1]
			switch heading {
			case "适用告警":
				listTarget = &doc.ApplicableAlerts
			case "关键词":
				listTarget = &doc.Keywords
			case "关键指标":
				listTarget = &doc.Metrics
			default:
				listTarget = nil
			}
		case strings.HasPrefix(line, "- ") && listTarget != nil:
			addUnique(listTarget, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		default:
			if current != nil && line != "" {
				if current.Text != "" {
					current.Text += "\n"
				}
				current.Text += strings.TrimSpace(raw)
			}
		}
	}

	if strings.TrimSpace(doc.Title) == "" {
		return Document{}, fmt.Errorf("parse runbook %s: title is required", filepath.Base(file))
	}
	if len(doc.ApplicableAlerts) == 0 {
		doc.ApplicableAlerts = []string{doc.Title}
	}
	return doc, nil
}

func addUnique(target *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *target {
		if strings.EqualFold(existing, value) {
			return
		}
	}
	*target = append(*target, value)
}
