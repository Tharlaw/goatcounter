// Copyright © 2019 Martin Tournoij <martin@arp242.net>
// This file is part of GoatCounter and published under the terms of the EUPL
// v1.2, which can be found in the LICENSE file or at http://eupl12.zgo.at

package goatcounter

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"zgo.at/blackmail"
	"zgo.at/goatcounter/cfg"
	"zgo.at/zlog"
	"zgo.at/zstd/zcrypto"
	"zgo.at/zstd/zfloat"
)

func ExportFile(site *Site) string {
	return fmt.Sprintf("%s/goatcounter-export-%s.csv.gz", os.TempDir(), site.Code)
}

// Export all data to a CSV file.
func Export(ctx context.Context, fp *os.File, last int64) {
	site := MustGetSite(ctx)
	l := zlog.Module("export").Field("site", site.ID).Field("last", last)
	l.Print("export started")

	gzfp := gzip.NewWriter(fp)
	defer fp.Close() // No need to error-check; just for safety.
	defer gzfp.Close()

	c := csv.NewWriter(gzfp)
	c.Write([]string{"Path", "Title", "Event", "Bot", "Session",
		"Referrer", "Browser", "Screen size", "Location", "Date"})

	var (
		err  error
		rows int
	)
	for {
		var hits Hits
		last, err = hits.List(ctx, 5000, last)
		if len(hits) == 0 {
			break
		}
		if err != nil {
			break
		}

		rows += len(hits)

		for _, hit := range hits {
			c.Write([]string{hit.Path, hit.Title, fmt.Sprintf("%t", hit.Event),
				fmt.Sprintf("%d", hit.Bot), fmt.Sprintf("%d", hit.Session),
				hit.Ref, hit.Browser, zfloat.Join(hit.Size, ","),
				hit.Location, hit.CreatedAt.Format(time.RFC3339)})
		}

		c.Flush()
		err = c.Error()
		if err != nil {
			break
		}

		// Small amount of breathing space.
		if cfg.Prod {
			time.Sleep(500 * time.Millisecond)
		}
	}

	if err != nil {
		l.Error(err)
		_ = gzfp.Close()
		_ = fp.Close()
		_ = os.Remove(fp.Name())
		return
	}

	err = gzfp.Close()
	if err != nil {
		l.Error(err)
		return
	}
	err = fp.Sync() // Ensure stat is correct.
	if err != nil {
		l.Error(err)
		return
	}

	stat, err := fp.Stat()
	size := "0"
	if err == nil {
		size = fmt.Sprintf("%.1f", float64(stat.Size())/1024/1024)
	}

	err = fp.Close()
	if err != nil {
		l.Error(err)
		return
	}

	f := ExportFile(site)
	err = os.Rename(fp.Name(), f)
	if err != nil {
		l.Error(err)
		return
	}

	hash, err := zcrypto.HashFile(f)
	if err != nil {
		l.Error(err)
		return
	}

	user := GetUser(ctx)
	err = blackmail.Send("GoatCounter export ready",
		blackmail.From("GoatCounter export", cfg.EmailFrom),
		blackmail.To(user.Email),
		blackmail.BodyMustText(EmailTemplate("email_export_done.gotxt", struct {
			Site   Site
			LastID int64
			Size   string
			Rows   int
			Hash   string
		}{*site, last, size, rows, hash})))
	if err != nil {
		l.Error(err)
		return
	}
}
