package events

import (
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
)

func (s *Service) GetTags(c *gin.Context) {
	year := c.Query("year")
	query := "SELECT tags FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND tags != ''"
	args := []any{}
	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	tagCounts := make(map[string]int)
	for rows.Next() {
		var tagsStr string
		rows.Scan(&tagsStr)
		for t := range strings.SplitSeq(tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tagCounts[t]++
			}
		}
	}
	type TagItem struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	result := make([]TagItem, 0, len(tagCounts))
	for name, count := range tagCounts {
		result = append(result, TagItem{Name: name, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	c.JSON(http.StatusOK, result)
}

func (s *Service) RenameTag(c *gin.Context) {
	var input struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.OldName == "" || input.NewName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both old_name and new_name are required"})
		return
	}
	rows, err := s.DB.Query("SELECT id, tags FROM timeline_events WHERE tags LIKE ?", "%"+input.OldName+"%")
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	updated := 0
	for rows.Next() {
		var id int
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			continue
		}
		parts := strings.Split(tags, ",")
		newParts := make([]string, 0, len(parts))
		changed := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == input.OldName {
				newParts = append(newParts, input.NewName)
				changed = true
			} else {
				newParts = append(newParts, p)
			}
		}
		if changed {
			newTags := strings.Join(newParts, ", ")
			s.DB.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
			updated++
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": updated})
}

func (s *Service) DeleteTag(c *gin.Context) {
	var input struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tag name is required"})
		return
	}
	rows, err := s.DB.Query("SELECT id, tags FROM timeline_events WHERE tags LIKE ?", "%"+input.Name+"%")
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	updated := 0
	for rows.Next() {
		var id int
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			continue
		}
		parts := strings.Split(tags, ",")
		newParts := make([]string, 0, len(parts))
		changed := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == input.Name {
				changed = true
			} else {
				newParts = append(newParts, p)
			}
		}
		if changed {
			newTags := strings.Join(newParts, ", ")
			s.DB.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
			updated++
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": updated})
}

func (s *Service) MergeTags(c *gin.Context) {
	var input struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Source == "" || input.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both source and target are required"})
		return
	}
	rows, err := s.DB.Query("SELECT id, tags FROM timeline_events WHERE tags LIKE ?", "%"+input.Source+"%")
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	updated := 0
	for rows.Next() {
		var id int
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			continue
		}
		parts := strings.Split(tags, ",")
		newParts := make([]string, 0, len(parts))
		changed := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == input.Source {
				if input.Target != "" && !slices.Contains(newParts, input.Target) {
					newParts = append(newParts, input.Target)
				}
				changed = true
			} else if p != "" {
				newParts = append(newParts, p)
			}
		}
		if changed {
			newTags := strings.Join(newParts, ", ")
			s.DB.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
			updated++
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": updated})
}
