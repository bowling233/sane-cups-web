# Scanner & Printer Web Hub

A lightweight, self-hosted web interface and REST API for network and local flatbed scanners and CUPS printers. Built with **Go** and **Vanilla JS/CSS** (Dockerized).

---

## ✨ Features

- **⚡ Quick Scanner**: Acquire high-resolution scans (150 / 300 / 600 DPI, Color / Grayscale / B&W Lineart) directly into PNG, JPEG, or PDF.
- **📑 Multi-Page PDF Creator**: Scan multiple physical pages sequentially with live thumbnail previews and compile them into a unified PDF document.
- **🖼️ Document Gallery**: Browse, search, preview in-browser, download, and manage past scanned documents.
- **🖨️ CUPS Print Center**: Upload documents (PDF, PNG, JPG, TXT) or re-print past scans directly to CUPS print queues with copies configuration and job queue monitoring.
- **🔍 Dynamic Auto-Discovery & Static IP Modes**: Automatically discovers network scanners (via SANE / mDNS / WSD) or allows configuring a static IP.
- **🚀 100% Pure Go Processing**: Single & multi-page PDF generation without heavyweight external Python/Pillow dependencies.
- **🎨 Sleek Modern UI**: Clean, neutral, responsive interface with drag-and-drop uploads and scan animations.

---

## 🚀 Quick Start (Docker Compose)

### 1. `docker-compose.yaml`

```yaml
services:
  printer:
    build: .
    container_name: printer-web-hub
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./scans:/app/scans
      - /var/run/cups/cups.sock:/var/run/cups/cups.sock:ro
      - ./config.yaml:/app/config.yaml:ro
```

### 2. Start the Service

```bash
docker compose up -d --build
```

Access the web interface at **`http://localhost:8085`** (or your server's IP address).

---

## ⚙️ Configuration

All application and device settings live in `/app/config.yaml`. Copy
`config.example.yaml` to `config.yaml` and edit it before starting the service.
Each item in `devices` represents a logical scanner, printer, or multifunction
device. Native `escl` scanning supports HTTP or HTTPS, optional Basic
authentication, normal CA verification, a custom CA, or a SHA-256 certificate
pin. Set `tls.verify: false` when compatibility with a self-signed device is
preferred. The `sane` scan driver remains available for other SANE backends.
CUPS controls whether a configured print queue uses IPP, IPPS, or authentication.


---

## 📡 REST API Endpoints

- `GET /api/status` - Query scanner connectivity and CUPS printer queue state.
- `POST /api/scan` - Trigger a single-page scan (`{ dpi, mode, format, custom_name }`).
- `POST /api/scan/multipage/start` - Initialize a multi-page scan session.
- `POST /api/scan/multipage/page` - Add a page to the active session.
- `POST /api/scan/multipage/finish` - Merge session pages into a compiled PDF.
- `POST /api/scan/multipage/cancel` - Discard an active multi-page session.
- `GET /api/scans` - List all archived scanned documents.
- `GET /api/scans/{filename}` - View / download a specific scanned document.
- `DELETE /api/scans/{filename}` - Delete a document from the archive.
- `GET /api/printers` - Query active CUPS queues and pending print jobs.
- `POST /api/print` - Submit a document (multipart file or existing scan) to print.
- `POST /api/print/cancel` - Cancel a specific print job or all active jobs.

---

## 📄 License

MIT License
