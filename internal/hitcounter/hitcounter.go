package hitcounter

import (
	"database/sql"
	"log/slog"
	"net/netip"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

type Config struct {
	DatabasePath string
	BufferSize   int
}

type HitEvent struct {
	IP          netip.Addr
	UserAgent   string
	HttpVersion string
	Scheme      string
	Country     string
	ASN         uint
}

type LoggedHit struct {
	IPVersion   string
	UserAgent   string
	HTTPVersion string
	Scheme      string
	Country     string
	ASN         uint
}

type HitCounter struct {
	db     *sql.DB
	events chan HitEvent
	done   chan struct{}
}

func New(config Config) (*HitCounter, error) {
	db, err := sql.Open("sqlite3", config.DatabasePath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS hits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ip TEXT NOT NULL,
			ip_version TEXT NOT NULL,
			user_agent TEXT,
			http_version TEXT,
			scheme TEXT,
			country TEXT,
			asn INTEGER,
			last_seen_on DATE NOT NULL,
			hit_count INTEGER NOT NULL DEFAULT 1
		);
		CREATE UNIQUE INDEX IF NOT EXISTS unique_hits ON hits (ip, user_agent);
	`); err != nil {
		return nil, err
	}

	hc := &HitCounter{
		db:     db,
		events: make(chan HitEvent, config.BufferSize),
		done:   make(chan struct{}),
	}

	go hc.run()
	return hc, nil
}

func (hc *HitCounter) LogHit(e HitEvent) {
	select {
	case hc.events <- e:
	default:
		slog.Warn("Hit counter buffer is full; dropping event")
	}
}

func (hc *HitCounter) ListRecent(limit int) ([]LoggedHit, error) {
	rows, err := hc.db.Query(`
		SELECT
			ip_version,
			user_agent,
			http_version,
			scheme,
			country,
			asn
		FROM hits
		ORDER BY last_seen_on DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]LoggedHit, 0)
	for rows.Next() {
		hit := LoggedHit{}
		err := rows.Scan(&hit.IPVersion, &hit.UserAgent, &hit.HTTPVersion, &hit.Scheme, &hit.Country, &hit.ASN)
		if err != nil {
			slog.Error("Failed to read logged hit", "err", err)
		}
		hits = append(hits, hit)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

func (hc *HitCounter) Close() error {
	close(hc.events)
	<-hc.done
	return hc.db.Close()
}

func (hc *HitCounter) run() {
	const (
		flushEvery = 2 * time.Second
		maxBatch   = 200
	)

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	batch := make([]HitEvent, 0, maxBatch)

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := hc.insertBatch(batch); err != nil {
			slog.Warn("Hit counter flush failed", "err", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e, ok := <-hc.events:
			if !ok {
				flush()
				return
			}
			batch = append(batch, e)
			if len(batch) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (hc *HitCounter) insertBatch(batch []HitEvent) error {
	tx, err := hc.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO hits (ip, ip_version, user_agent, http_version, scheme, country, asn, last_seen_on, hit_count)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ip, user_agent) DO UPDATE SET hit_count=hit_count+1, last_seen_on=?;
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range batch {
		ipVersion := ""
		ipString := ""

		if e.IP.Is4() {
			ipVersion = "IPv4"
			ipString = e.IP.String()
		}
		if e.IP.Is6() {
			ipVersion = "IPv6"
			ipString = netip.PrefixFrom(e.IP, 64).Masked().String()
		}

		if ipString == "" || ipVersion == "" {
			slog.Warn("Invalid IP address?", "ipaddr", e.IP)
			continue
		}

		if e.UserAgent == "" {
			slog.Warn("No user agent")
			continue
		}

		today := time.Now().UTC().Format(time.DateOnly)
		if _, err := stmt.Exec(
			ipString,
			ipVersion,
			e.UserAgent,
			e.HttpVersion,
			e.Scheme,
			e.Country,
			e.ASN,
			today,
			1,
			today); err != nil {
			slog.Error("Failed to log hit", "err", err)
			return err
		}
	}
	return tx.Commit()
}
