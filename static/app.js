// Scanner & Printer Web Application Client
document.addEventListener('DOMContentLoaded', () => {
    // State
    let currentTab = 'tab-scan';
    let currentSessionId = null;
    let sessionPages = [];
    let galleryScans = [];
    let isScanning = false;
    let selectedPrintFile = null;
    let currentModalScan = null;

    // Elements
    const loginSection = document.getElementById('loginSection');
    const dashboardSection = document.getElementById('dashboardSection');
    const loginForm = document.getElementById('loginForm');
    const loginUsername = document.getElementById('loginUsername');
    const loginPassword = document.getElementById('loginPassword');
    const loginErrorAlert = document.getElementById('loginErrorAlert');
    const authContainer = document.getElementById('authContainer');
    const loggedInUserBadge = document.getElementById('loggedInUserBadge');
    const logoutBtn = document.getElementById('logoutBtn');

    const scannerDot = document.getElementById('scannerDot');
    const scannerStatusText = document.getElementById('scannerStatusText');
    const printerDot = document.getElementById('printerDot');
    const printerStatusText = document.getElementById('printerStatusText');
    const refreshStatusBtn = document.getElementById('refreshStatusBtn');

    const tabButtons = document.querySelectorAll('.nav-tab');
    const tabPanels = document.querySelectorAll('.tab-panel');

    // Single Scan Elements
    const singleScanForm = document.getElementById('singleScanForm');
    const scanDpiSelect = document.getElementById('scanDpiSelect');
    const scanModeSelect = document.getElementById('scanModeSelect');
    const scanFormatSelect = document.getElementById('scanFormatSelect');
    const scanCustomName = document.getElementById('scanCustomName');
    const startScanBtn = document.getElementById('startScanBtn');
    const scanningOverlay = document.getElementById('scanningOverlay');
    const previewPlaceholder = document.getElementById('previewPlaceholder');
    const previewImage = document.getElementById('previewImage');
    const previewPdfFrame = document.getElementById('previewPdfFrame');
    const previewActions = document.getElementById('previewActions');
    const downloadPreviewBtn = document.getElementById('downloadPreviewBtn');
    const printPreviewBtn = document.getElementById('printPreviewBtn');

    // Multi-page Elements
    const mpDpi = document.getElementById('mpDpi');
    const mpMode = document.getElementById('mpMode');
    const mpDocName = document.getElementById('mpDocName');
    const mpScanPageBtn = document.getElementById('mpScanPageBtn');
    const mpPagesStrip = document.getElementById('mpPagesStrip');
    const mpCancelBtn = document.getElementById('mpCancelBtn');
    const mpFinishBtn = document.getElementById('mpFinishBtn');
    const mpPageCountBadge = document.getElementById('mpPageCountBadge');

    // Gallery Elements
    const galleryTableBody = document.getElementById('galleryTableBody');
    const gallerySearchInput = document.getElementById('gallerySearchInput');
    const refreshGalleryBtn = document.getElementById('refreshGalleryBtn');
    const galleryCountBadge = document.getElementById('galleryCountBadge');

    // Print Center Elements
    const printUploadForm = document.getElementById('printUploadForm');
    const printDropzone = document.getElementById('printDropzone');
    const printFileInput = document.getElementById('printFileInput');
    const selectedPrintFileName = document.getElementById('selectedPrintFileName');
    const printCopies = document.getElementById('printCopies');
    const printDestinationQueue = document.getElementById('printDestinationQueue');
    const submitPrintBtn = document.getElementById('submitPrintBtn');
    const printerQueueContainer = document.getElementById('printerQueueContainer');
    const rawCupsStatus = document.getElementById('rawCupsStatus');
    const cancelAllJobsBtn = document.getElementById('cancelAllJobsBtn');

    // Modal Elements
    const previewModal = document.getElementById('previewModal');
    const modalTitle = document.getElementById('modalTitle');
    const modalBody = document.getElementById('modalBody');
    const modalDownloadBtn = document.getElementById('modalDownloadBtn');
    const modalPrintBtn = document.getElementById('modalPrintBtn');
    const modalCloseBtn = document.getElementById('modalCloseBtn');
    const toastContainer = document.getElementById('toastContainer');

    // Toast Notifications
    function showToast(message, type = 'info') {
        const toast = document.createElement('div');
        toast.className = `toast ${type}`;
        toast.textContent = message;
        toastContainer.appendChild(toast);
        setTimeout(() => {
            toast.style.opacity = '0';
            toast.style.transition = 'opacity 0.3s ease';
            setTimeout(() => toast.remove(), 300);
        }, 4000);
    }

    // Tabs Switching
    tabButtons.forEach(btn => {
        btn.addEventListener('click', () => {
            const target = btn.getAttribute('data-tab');
            tabButtons.forEach(b => b.classList.remove('active'));
            tabPanels.forEach(p => p.classList.remove('active'));

            btn.classList.add('active');
            const panel = document.getElementById(target);
            if (panel) panel.classList.add('active');
            currentTab = target;

            if (target === 'tab-gallery') loadScansGallery();
            if (target === 'tab-printer') loadPrinterStatus();
        });
    });

    // Hardware Status Check
    async function checkStatus() {
        try {
            const res = await fetch('/api/status');
            if (!res.ok) throw new Error('Status query failed');
            const data = await res.json();

            // Scanner Status
            if (data.scanner && data.scanner.online) {
                scannerDot.className = 'status-dot online';
                scannerStatusText.textContent = 'Scanner: Ready';
            } else {
                scannerDot.className = 'status-dot offline';
                scannerStatusText.textContent = 'Scanner: Offline';
            }

            // Printer Status
            if (data.printer && data.printer.online) {
                printerDot.className = 'status-dot online';
                const jobsText = data.printer.active_jobs > 0 ? ` (${data.printer.active_jobs} jobs)` : '';
                printerStatusText.textContent = `Printer: Ready${jobsText}`;
                if (printDestinationQueue && data.printer.name) {
                    printDestinationQueue.value = data.printer.name;
                }
            } else {
                printerDot.className = 'status-dot offline';
                printerStatusText.textContent = 'Printer: Offline';
            }

            if (data.printer && data.printer.raw_status) {
                rawCupsStatus.textContent = data.printer.raw_status;
            }
        } catch (err) {
            scannerDot.className = 'status-dot offline';
            scannerStatusText.textContent = 'Scanner: Disconnected';
            printerDot.className = 'status-dot offline';
            printerStatusText.textContent = 'Printer: Disconnected';
        }
    }

    refreshStatusBtn.addEventListener('click', () => {
        showToast('Refreshing hardware status...', 'info');
        checkStatus();
    });

    // Single Scan Execution
    singleScanForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        if (isScanning) return;

        const dpi = parseInt(scanDpiSelect.value, 10);
        const mode = scanModeSelect.value;
        const format = scanFormatSelect.value;
        const customName = scanCustomName.value.trim();

        setScanningState(true);

        try {
            const res = await fetch('/api/scan', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ dpi, mode, format, custom_name: customName })
            });

            const result = await res.json();
            if (!res.ok || result.error) {
                throw new Error(result.message || 'Scanning encountered an error');
            }

            showToast(`Scan saved: ${result.filename} (${result.size_formatted})`, 'success');
            displaySinglePreview(result.url, format, result.filename);
            loadScansGallery();

        } catch (err) {
            showToast(err.message, 'error');
        } finally {
            setScanningState(false);
        }
    });

    function setScanningState(scanning) {
        isScanning = scanning;
        startScanBtn.disabled = scanning;
        startScanBtn.textContent = scanning ? 'Scanning in progress...' : 'Scan Document';
        scanningOverlay.style.display = scanning ? 'flex' : 'none';
    }

    function displaySinglePreview(url, format, filename) {
        previewPlaceholder.style.display = 'none';
        previewActions.style.display = 'flex';

        downloadPreviewBtn.href = url + '?download=true';
        downloadPreviewBtn.setAttribute('download', filename);

        printPreviewBtn.onclick = () => printExistingScan(filename);

        if (format === 'pdf') {
            previewImage.style.display = 'none';
            previewPdfFrame.style.display = 'block';
            previewPdfFrame.src = url;
        } else {
            previewPdfFrame.style.display = 'none';
            previewImage.style.display = 'block';
            previewImage.src = url + '?t=' + Date.now();
        }
    }

    // Multi-Page Session Handler
    mpScanPageBtn.addEventListener('click', async () => {
        if (isScanning) return;

        if (!currentSessionId) {
            try {
                const res = await fetch('/api/scan/multipage/start', { method: 'POST' });
                const data = await res.json();
                if (!res.ok || data.error) throw new Error(data.message || 'Failed to initialize session');
                currentSessionId = data.session_id;
                sessionPages = [];
                updateMultiPageUI();
            } catch (err) {
                showToast(err.message, 'error');
                return;
            }
        }

        const dpi = parseInt(mpDpi.value, 10);
        const mode = mpMode.value;

        mpScanPageBtn.disabled = true;
        mpScanPageBtn.textContent = 'Scanning page...';

        try {
            const res = await fetch('/api/scan/multipage/page', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ session_id: currentSessionId, dpi, mode })
            });

            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.message || 'Failed to scan page');

            sessionPages.push(data);
            showToast(`Scanned page ${data.page_number}`, 'success');
            updateMultiPageUI();

        } catch (err) {
            showToast(err.message, 'error');
        } finally {
            mpScanPageBtn.disabled = false;
            mpScanPageBtn.textContent = 'Scan Next Page';
        }
    });

    mpCancelBtn.addEventListener('click', async () => {
        if (!currentSessionId) return;
        if (!confirm('Cancel this multi-page session and delete temporary pages?')) return;

        try {
            await fetch('/api/scan/multipage/cancel', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ session_id: currentSessionId })
            });
            showToast('Session cancelled', 'info');
        } catch (e) {
            // Ignore error
        }

        currentSessionId = null;
        sessionPages = [];
        updateMultiPageUI();
    });

    mpFinishBtn.addEventListener('click', async () => {
        if (!currentSessionId || sessionPages.length === 0) return;

        const customName = mpDocName.value.trim();
        mpFinishBtn.disabled = true;
        mpFinishBtn.textContent = 'Compiling PDF...';

        try {
            const res = await fetch('/api/scan/multipage/finish', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ session_id: currentSessionId, custom_name: customName })
            });

            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.message || 'PDF compilation failed');

            showToast(`Compiled PDF: ${data.filename} (${data.size_formatted})`, 'success');
            currentSessionId = null;
            sessionPages = [];
            mpDocName.value = '';
            updateMultiPageUI();
            loadScansGallery();

            // Switch to gallery tab
            const galTab = document.querySelector('[data-tab="tab-gallery"]');
            if (galTab) galTab.click();

        } catch (err) {
            showToast(err.message, 'error');
        } finally {
            mpFinishBtn.disabled = false;
            mpFinishBtn.textContent = 'Compile PDF';
        }
    });

    function updateMultiPageUI() {
        mpPageCountBadge.textContent = sessionPages.length;
        if (sessionPages.length > 0) {
            mpCancelBtn.style.display = 'inline-flex';
            mpFinishBtn.style.display = 'inline-flex';
            mpPagesStrip.replaceChildren();

            sessionPages.forEach(p => {
                const item = document.createElement('div');
                item.className = 'page-item';

                const img = document.createElement('img');
                img.src = p.url;
                img.alt = `Page ${p.page_number}`;

                const label = document.createElement('div');
                label.className = 'page-item-label';
                label.textContent = `Page ${p.page_number}`;

                item.appendChild(img);
                item.appendChild(label);
                mpPagesStrip.appendChild(item);
            });
        } else {
            mpCancelBtn.style.display = 'none';
            mpFinishBtn.style.display = 'none';
            mpPagesStrip.innerHTML = '<p class="empty-state">No pages scanned in current session. Click <strong>Scan Next Page</strong> to begin.</p>';
        }
    }

    // Documents Table
    async function loadScansGallery() {
        try {
            const res = await fetch('/api/scans');
            if (!res.ok) throw new Error('Failed to load gallery');
            const data = await res.json();
            galleryScans = data.scans || [];
            galleryCountBadge.textContent = data.count || 0;
            renderGallery(galleryScans);
        } catch (err) {
            galleryTableBody.innerHTML = `<tr><td colspan="4" class="empty-cell">Error loading scans: ${err.message}</td></tr>`;
        }
    }

    function renderGallery(scans) {
        galleryTableBody.replaceChildren();
        if (!scans || scans.length === 0) {
            const tr = document.createElement('tr');
            const td = document.createElement('td');
            td.colSpan = 4;
            td.className = 'empty-cell';
            td.textContent = 'No scanned documents found.';
            tr.appendChild(td);
            galleryTableBody.appendChild(tr);
            return;
        }

        scans.forEach(scan => {
            const tr = document.createElement('tr');

            // Name column
            const tdName = document.createElement('td');
            const link = document.createElement('a');
            link.href = '#';
            link.textContent = scan.name;
            link.style.fontWeight = '600';
            link.style.color = 'var(--primary)';
            link.style.textDecoration = 'none';
            link.onclick = (e) => {
                e.preventDefault();
                openModal(scan.url, scan.name, scan.format);
            };
            tdName.appendChild(link);

            // Format column
            const tdFormat = document.createElement('td');
            tdFormat.textContent = scan.format.toUpperCase();

            // Size column
            const tdSize = document.createElement('td');
            tdSize.textContent = scan.size_formatted;

            // Actions column
            const tdActions = document.createElement('td');
            const divActions = document.createElement('div');
            divActions.className = 'table-actions';

            const dlBtn = document.createElement('a');
            dlBtn.className = 'btn btn-sm btn-outline';
            dlBtn.href = scan.url + '?download=true';
            dlBtn.setAttribute('download', scan.name);
            dlBtn.textContent = 'Download';

            const printBtn = document.createElement('button');
            printBtn.className = 'btn btn-sm btn-outline';
            printBtn.textContent = 'Print';
            printBtn.onclick = () => printExistingScan(scan.name);

            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-sm btn-danger';
            delBtn.textContent = 'Delete';
            delBtn.onclick = () => deleteScan(scan.name);

            divActions.appendChild(dlBtn);
            divActions.appendChild(printBtn);
            divActions.appendChild(delBtn);
            tdActions.appendChild(divActions);

            tr.appendChild(tdName);
            tr.appendChild(tdFormat);
            tr.appendChild(tdSize);
            tr.appendChild(tdActions);
            galleryTableBody.appendChild(tr);
        });
    }

    gallerySearchInput.addEventListener('input', (e) => {
        const q = e.target.value.toLowerCase().trim();
        if (!q) {
            renderGallery(galleryScans);
        } else {
            const filtered = galleryScans.filter(s => s.name.toLowerCase().includes(q));
            renderGallery(filtered);
        }
    });

    refreshGalleryBtn.addEventListener('click', loadScansGallery);

    async function deleteScan(filename) {
        if (!confirm(`Are you sure you want to delete "${filename}"?`)) return;

        try {
            const res = await fetch(`/api/scans/${encodeURIComponent(filename)}`, { method: 'DELETE' });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.message || 'Delete failed');

            showToast(`Deleted ${filename}`, 'success');
            loadScansGallery();
        } catch (err) {
            showToast(err.message, 'error');
        }
    }

    // Modal Preview Handler
    function openModal(url, filename, format) {
        currentModalScan = { url, filename, format };
        modalTitle.textContent = filename;
        modalDownloadBtn.href = url + '?download=true';
        modalDownloadBtn.setAttribute('download', filename);
        modalPrintBtn.onclick = () => printExistingScan(filename);

        modalBody.replaceChildren();

        if (format === 'pdf') {
            const iframe = document.createElement('iframe');
            iframe.src = url;
            iframe.style.width = '100%';
            iframe.style.height = '65vh';
            iframe.style.border = 'none';
            modalBody.appendChild(iframe);
        } else {
            const img = document.createElement('img');
            img.src = url;
            img.alt = filename;
            modalBody.appendChild(img);
        }

        previewModal.classList.add('active');
    }

    function closeModal() {
        previewModal.classList.remove('active');
        modalBody.replaceChildren();
        currentModalScan = null;
    }

    modalCloseBtn.addEventListener('click', closeModal);
    previewModal.addEventListener('click', (e) => {
        if (e.target === previewModal) closeModal();
    });

    // Print Center
    printDropzone.addEventListener('click', () => printFileInput.click());

    printFileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            handleSelectedPrintFile(e.target.files[0]);
        }
    });

    printDropzone.addEventListener('dragover', (e) => {
        e.preventDefault();
        printDropzone.classList.add('dragover');
    });

    printDropzone.addEventListener('dragleave', () => {
        printDropzone.classList.remove('dragover');
    });

    printDropzone.addEventListener('drop', (e) => {
        e.preventDefault();
        printDropzone.classList.remove('dragover');
        if (e.dataTransfer.files.length > 0) {
            handleSelectedPrintFile(e.dataTransfer.files[0]);
        }
    });

    function handleSelectedPrintFile(file) {
        selectedPrintFile = file;
        selectedPrintFileName.textContent = `Selected: ${file.name} (${(file.size / 1024 / 1024).toFixed(2)} MB)`;
        selectedPrintFileName.style.display = 'block';
        submitPrintBtn.disabled = false;
    }

    printUploadForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        if (!selectedPrintFile) return;

        const copies = parseInt(printCopies.value, 10) || 1;
        const formData = new FormData();
        formData.append('file', selectedPrintFile);
        formData.append('copies', copies.toString());

        submitPrintBtn.disabled = true;
        showToast('Submitting print job to queue...', 'info');

        try {
            const res = await fetch('/api/print', {
                method: 'POST',
                body: formData
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.message || 'Print job failed');

            showToast(`Print job submitted successfully`, 'success');
            printUploadForm.reset();
            selectedPrintFile = null;
            selectedPrintFileName.style.display = 'none';
            submitPrintBtn.disabled = true;
            loadPrinterStatus();

        } catch (err) {
            showToast(err.message, 'error');
        } finally {
            submitPrintBtn.disabled = false;
        }
    });

    async function printExistingScan(filename) {
        const copies = prompt(`Send "${filename}" to printer? Number of copies:`, '1');
        if (copies === null) return;
        const c = parseInt(copies, 10);
        if (isNaN(c) || c < 1) return;

        showToast(`Sending ${filename} to printer...`, 'info');

        try {
            const res = await fetch('/api/print', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ scan_filename: filename, copies: c })
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.message || 'Print job failed');

            showToast(`Print job created: ${data.message}`, 'success');
            loadPrinterStatus();
        } catch (err) {
            showToast(err.message, 'error');
        }
    }

    async function loadPrinterStatus() {
        try {
            const res = await fetch('/api/printers');
            if (!res.ok) throw new Error('Failed to query printer');
            const data = await res.json();

            rawCupsStatus.textContent = data.status_text || 'No status available';
            printerQueueContainer.replaceChildren();

            if (!data.jobs || data.jobs.length === 0) {
                printerQueueContainer.innerHTML = '<p class="empty-state">No pending jobs in print queue.</p>';
            } else {
                data.jobs.forEach(job => {
                    const jobItem = document.createElement('div');
                    jobItem.className = 'queue-item';
                    jobItem.textContent = job;
                    printerQueueContainer.appendChild(jobItem);
                });
            }
        } catch (err) {
            rawCupsStatus.textContent = 'Error querying CUPS: ' + err.message;
        }
    }

    cancelAllJobsBtn.addEventListener('click', async () => {
        if (!confirm('Cancel all pending print jobs?')) return;
        try {
            const res = await fetch('/api/print/cancel', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({})
            });
            const data = await res.json();
            showToast(data.message || 'Jobs cancelled', 'info');
            loadPrinterStatus();
        } catch (err) {
            showToast(err.message, 'error');
        }
    });

    // Authentication Lifecycle
    async function checkAuthAndInit() {
        try {
            const res = await fetch('/api/auth/status');
            const data = await res.json();

            if (data.auth_required && !data.logged_in) {
                loginSection.style.display = 'block';
                dashboardSection.style.display = 'none';
                authContainer.style.display = 'none';
                const statusPills = document.getElementById('headerStatusBar');
                if (statusPills) statusPills.style.display = 'none';
            } else {
                loginSection.style.display = 'none';
                dashboardSection.style.display = 'block';
                const statusPills = document.getElementById('headerStatusBar');
                if (statusPills) statusPills.style.display = 'flex';

                if (data.auth_required) {
                    authContainer.style.display = 'flex';
                    loggedInUserBadge.textContent = data.user || 'admin';
                } else {
                    authContainer.style.display = 'none';
                }

                checkStatus();
                loadScansGallery();
            }
        } catch (err) {
            checkStatus();
            loadScansGallery();
        }
    }

    if (loginForm) {
        loginForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            const username = loginUsername.value.trim();
            const password = loginPassword.value;
            const submitBtn = document.getElementById('loginSubmitBtn');

            submitBtn.disabled = true;
            loginErrorAlert.style.display = 'none';

            try {
                const res = await fetch('/api/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username, password })
                });
                const data = await res.json();
                if (!res.ok || data.error) {
                    throw new Error(data.message || 'Invalid username or password');
                }

                showToast(`Signed in as ${data.user}`, 'success');
                loginForm.reset();
                checkAuthAndInit();
            } catch (err) {
                loginErrorAlert.textContent = err.message;
                loginErrorAlert.style.display = 'block';
            } finally {
                submitBtn.disabled = false;
            }
        });
    }

    if (logoutBtn) {
        logoutBtn.addEventListener('click', async () => {
            try {
                await fetch('/api/logout', { method: 'POST' });
                showToast('Signed out', 'info');
                checkAuthAndInit();
            } catch (err) {
                window.location.reload();
            }
        });
    }

    // Startup
    checkAuthAndInit();
    setInterval(() => {
        if (dashboardSection && dashboardSection.style.display !== 'none') {
            checkStatus();
        }
    }, 15000);
});
