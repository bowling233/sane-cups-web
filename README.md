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
    environment:
      - CUPS_SERVER=/var/run/cups/cups.sock
      # Auto-discovery enabled by default:
      - AUTO_DISCOVER=true
      # Optional: specify static scanner IP
      # - SCANNER_IP=[IP_ADDRESS]
      # Optional: override default printer queue name
      # - DEFAULT_PRINTER=My_Printer_Queue
```

### 2. Start the Service

```bash
docker compose up -d --build
```

Access the web interface at **`http://localhost:8085`** (or your server's IP address).

---

## ⚙️ Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PORT` | Web server listening port | `8085` |
| `AUTO_DISCOVER` | Enable/disable network broadcast scanner discovery (`true` / `false`) | `true` |
| `SCANNER_IP` / `SCANNER_HOST` | Fixed IP address of the scanner | *(Auto-discovered)* |
| `DEFAULT_SCANNER` | Explicit SANE device string (e.g. `epsonds:net:[SCANNER_IP]`) | *(Auto-selected)* |
| `DEFAULT_PRINTER` | CUPS printer destination queue name | *(System default queue)* |
| `AUTH_USERNAME` | Username for basic authentication | `admin` |
| `AUTH_PASSWORD` | Password for basic authentication (leave blank for open access) | *(Disabled / Open)* |
| `CUPS_SERVER` | Path to CUPS socket or server host | `/var/run/cups/cups.sock` |

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
