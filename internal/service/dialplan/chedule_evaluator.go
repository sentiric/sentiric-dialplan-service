// sentiric-dialplan-service/internal/service/dialplan/chedule_evaluator.go
package dialplan

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
)

// ScheduleDefinition, veritabanındaki JSONB yapısını karşılar.
type ScheduleDefinition struct {
	Timezone string                 `json:"timezone"`
	Days     map[string][]TimeRange `json:"days"`     // "mon": [{"start": "09:00", "end": "18:00"}]
	Holidays []string               `json:"holidays"` //["2026-01-01"]
}

type TimeRange struct {
	Start string `json:"start"` // HH:MM
	End   string `json:"end"`   // HH:MM
}

// IsWorkingHour: Verilen zamanın çalışma saatleri içinde olup olmadığını kontrol eder.
// [ARCH-COMPLIANCE]: Trace ID barındıran Context Logger parametre olarak geçirildi.
func IsWorkingHour(scheduleJson string, l zerolog.Logger) bool {
	if scheduleJson == "" {
		return true // Takvim yoksa her zaman açık varsay
	}

	var sched ScheduleDefinition
	if err := json.Unmarshal([]byte(scheduleJson), &sched); err != nil {
		l.Error().Err(err).Str("event", logger.EventScheduleParseError).Msg("Schedule JSON parse hatası, varsayılan: AÇIK")
		return true
	}

	// 1. Timezone Ayarla
	loc, err := time.LoadLocation(sched.Timezone)
	if err != nil {
		l.Warn().Str("event", logger.EventScheduleParseError).Str("tz", sched.Timezone).Msg("Geçersiz Timezone, UTC kullanılıyor.")
		loc = time.UTC
	}

	now := time.Now().In(loc)

	// 2. Tatil Kontrolü (YYYY-MM-DD)
	todayStr := now.Format("2006-01-02")
	for _, holiday := range sched.Holidays {
		if holiday == todayStr {
			return false // Tatil günü -> KAPALI
		}
	}

	// 3. Gün Kontrolü (mon, tue...)
	weekday := strings.ToLower(now.Weekday().String()[:3]) // "mon", "tue"
	ranges, ok := sched.Days[weekday]
	if !ok || len(ranges) == 0 {
		return false // O gün için tanım yoksa -> KAPALI
	}

	// 4. Saat Aralığı Kontrolü
	currentMinutes := now.Hour()*60 + now.Minute()

	for _, rng := range ranges {
		startMin := parseTimeHeader(rng.Start)
		endMin := parseTimeHeader(rng.End)

		if currentMinutes >= startMin && currentMinutes < endMin {
			return true // Aralıklardan birine uyuyorsa -> AÇIK
		}
	}

	return false // Hiçbir aralığa uymadı -> KAPALI
}

// "09:30" formatını günün dakikasına çevirir (9*60 + 30 = 570)
func parseTimeHeader(hhmm string) int {
	parts := strings.Split(hhmm, ":")
	if len(parts) != 2 {
		return 0
	}
	h, _ := time.Parse("15", parts[0]) // Basit parsing trick
	m, _ := time.Parse("04", parts[1])
	return h.Hour()*60 + m.Minute()
}
