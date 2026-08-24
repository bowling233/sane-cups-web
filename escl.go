package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func esclClient(s *ScanConfig) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.TLS.ServerName}
	if !s.TLS.Verify {
		tlsConfig.InsecureSkipVerify = true
	} // Explicit compatibility option.
	if s.TLS.CAFile != "" {
		pem, err := os.ReadFile(s.TLS.CAFile)
		if err != nil {
			return nil, err
		}
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificate found in %s", s.TLS.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	if pin := normalizeFingerprint(s.TLS.SHA256Fingerprint); pin != "" {
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("scanner supplied no certificate")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if hex.EncodeToString(sum[:]) != pin {
				return fmt.Errorf("scanner certificate SHA-256 fingerprint mismatch")
			}
			return nil
		}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: 120 * time.Second}, nil
}

func normalizeFingerprint(v string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), ":", ""))
}

func esclRequest(ctx context.Context, c *http.Client, s *ScanConfig, method, url string, body io.Reader) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		r.Header.Set("Content-Type", "text/xml")
	}
	if s.Auth.Type == "basic" {
		r.SetBasicAuth(s.Auth.Username, s.Auth.Password)
	}
	return c.Do(r)
}

func executeESCLScan(ctx context.Context, s *ScanConfig, dpi int, mode, format, target string) error {
	c, err := esclClient(s)
	if err != nil {
		return err
	}
	color := "RGB24"
	if mode == "Gray" {
		color = "Grayscale8"
	}
	if mode == "Lineart" {
		color = "BlackAndWhite1"
	}
	docfmt := "image/png"
	if format == "jpg" || format == "jpeg" {
		docfmt = "image/jpeg"
	}
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><scan:ScanSettings xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:escl="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm"><pwg:Version>2.0</pwg:Version><pwg:ScanRegions><pwg:ScanRegion><pwg:ContentRegionUnits>escl:ThreeHundredthsOfInches</pwg:ContentRegionUnits><pwg:Height>3508</pwg:Height><pwg:Width>2480</pwg:Width><pwg:XOffset>0</pwg:XOffset><pwg:YOffset>0</pwg:YOffset></pwg:ScanRegion></pwg:ScanRegions><pwg:DocumentFormat>%s</pwg:DocumentFormat><scan:ColorMode>%s</scan:ColorMode><scan:XResolution>%d</scan:XResolution><scan:YResolution>%d</scan:YResolution><scan:InputSource>Platen</scan:InputSource></scan:ScanSettings>`, docfmt, color, dpi, dpi)
	base := strings.TrimRight(s.Endpoint, "/")
	resp, err := esclRequest(ctx, c, s, http.MethodPost, base+"/ScanJobs", strings.NewReader(xml))
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("eSCL create scan job: %s", resp.Status)
	}
	job := resp.Header.Get("Location")
	if job == "" {
		return fmt.Errorf("eSCL response has no scan job location")
	}
	if strings.HasPrefix(job, "/") {
		job = strings.TrimSuffix(base, "/eSCL") + job
	}
	defer func() {
		if r, e := esclRequest(context.Background(), c, s, http.MethodDelete, job, nil); e == nil {
			r.Body.Close()
		}
	}()
	resp, err = esclRequest(ctx, c, s, http.MethodGet, strings.TrimRight(job, "/")+"/NextDocument", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("eSCL retrieve document: %s", resp.Status)
	}
	tmp := target
	needsPDF := format == "pdf"
	if needsPDF {
		f, e := os.CreateTemp("", "escl-*.png")
		if e != nil {
			return e
		}
		tmp = f.Name()
		f.Close()
		defer os.Remove(tmp)
	}
	f, err := os.Create(filepath.Clean(tmp))
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if needsPDF {
		data, e := os.ReadFile(tmp)
		if e != nil {
			return e
		}
		return convertImagesToPDFInPureGo([][]byte{data}, dpi, target)
	}
	return nil
}
