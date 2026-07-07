package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func rawJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var values []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		values = append(values, s)
	}
	return values, rows.Err()
}

func ticketNumber() string {
	return fmt.Sprintf("REQ-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano()%1000000)
}

func (a *App) audit(actorID int64, action, entityType string, entityID int64, metadata any) {
	meta, _ := rawJSON(metadata)
	_, _ = a.db.Exec(`INSERT INTO audit_logs (actor_user_id, action, entity_type, entity_id, metadata_json) VALUES (?, ?, ?, ?, ?)`,
		actorID, action, entityType, entityID, meta)
}

func (a *App) notify(userID, requestID int64, typ, message string) {
	var req any
	if requestID > 0 {
		req = requestID
	}
	_, _ = a.db.Exec(`INSERT INTO notifications (user_id, request_id, type, message) VALUES (?, ?, ?, ?)`, userID, req, typ, message)
}
