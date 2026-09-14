// Radar Feature JS

// 状态变量
let radarTargets = [];
let radarEnabled = false;

const radarEnabledMessage = '系统会在后台静默轮询以下博主，发现新视频后会自动推入您的下载队列。(如果博主列表为空，请先在视频号添加博主)';
const radarDisabledMessage = '雷达未开启。请在 config.yaml 中将 radar_enabled 设置为 true 后重启程序，当前不会执行自动监控。';

// 初始化
document.addEventListener('DOMContentLoaded', () => {
    // 监听导航切换，若切到 radar 页面则刷新列表
    const navItems = document.querySelectorAll('.nav-item');
    navItems.forEach(item => {
        item.addEventListener('click', () => {
            if (item.dataset.page === 'radar') {
                loadRadarTargets();
                refreshBackfillStatus();
                loadBackfillSettings();
            }
        });
    });

    // 全量下载进度轮询（30秒一次，与雷达列表刷新错开）
    setInterval(() => {
        if (document.getElementById('page-radar') && document.getElementById('page-radar').style.display !== 'none') {
            refreshBackfillStatus();
        }
    }, 30000);
});

// 加载雷达监控目标
async function loadRadarTargets() {
    try {
        const [response, settingsResult] = await Promise.all([
            fetch('/api/v1/radar/targets'),
            ApiClient.getSettings().catch(() => null)
        ]);
        if (!response.ok) throw new Error('加载失败');

        radarEnabled = !!(settingsResult && settingsResult.success && settingsResult.data && settingsResult.data.radarEnabled);
        renderRadarGlobalStatus();

        const res = await response.json();
        if (res.code === 0 || res.code === 200) {
            radarTargets = res.data || [];
            renderRadarTable();
        } else {
            showMessage(res.message || '加载目标失败', 'error');
        }
    } catch (err) {
        console.error('加载监控目标失败:', err);
        showMessage('加载失败，请检查网络', 'error');
    }
}

function renderRadarGlobalStatus() {
    const alert = document.getElementById('radarGlobalStatusAlert');
    const text = document.getElementById('radarGlobalStatusText');
    const addButton = document.getElementById('radarAddButton');
    if (!alert || !text) return;

    if (radarEnabled) {
        alert.classList.remove('alert-warning');
        alert.classList.add('alert-info');
        text.textContent = radarEnabledMessage;
        if (addButton) {
            addButton.disabled = false;
            addButton.title = '';
        }
        return;
    }

    alert.classList.remove('alert-info');
    alert.classList.add('alert-warning');
    text.textContent = radarDisabledMessage;
    if (addButton) {
        addButton.disabled = false;
        addButton.title = '可先配置监控目标，开启 radar_enabled 并重启后生效';
    }
}

// 渲染雷达列表表格
function renderRadarTable() {
    const tbody = document.getElementById('radarTableBody');
    if (!tbody) return;

    if (radarTargets.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" style="text-align: center; color: var(--text-secondary); padding: 40px;">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width: 48px; height: 48px; margin: 0 auto 16px; opacity: 0.5;">
                        <circle cx="12" cy="12" r="10" />
                        <line x1="12" y1="8" x2="12" y2="12" />
                        <line x1="12" y1="16" x2="12.01" y2="16" />
                    </svg>
                    <p>暂无监控目标</p>
                </td>
            </tr>
        `;
        return;
    }

    tbody.innerHTML = radarTargets.map(target => {
        let statusClass = 'text-warning';
        let statusText = '雷达未开启';
        if (radarEnabled) {
            statusClass = target.status === 'active' ? 'text-success' : 'text-warning';
            statusText = target.status === 'active' ? '监控中' : '已暂停';
        }

        let lastCheck = '从未检测';
        if (target.last_check_time) {
            lastCheck = new Date(target.last_check_time).toLocaleString();
        }

        return `
            <tr>
                <td><strong>${escapeHtml(target.author_name)}</strong></td>
                <td>
                    <div style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${target.username}">
                        ${target.username}
                    </div>
                </td>
                <td>${target.interval_minutes} 分钟</td>
                <td><span style="font-size: 13px; color: var(--text-muted);">${lastCheck}</span></td>
                <td><span class="${statusClass}" style="font-weight: 500;">${statusText}</span></td>
                <td>
                    <div style="display: flex; gap: 6px; align-items: center;">
                        ${target.status === 'active'
                ? `<button class="btn btn-secondary" onclick="toggleRadarStatus('${target.id}', 'paused')" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">暂停</button>`
                : `<button class="btn btn-primary" onclick="toggleRadarStatus('${target.id}', 'active')" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">恢复</button>`
            }
                        <button class="btn btn-secondary" onclick="checkRadarNow('${target.id}', '${escapeHtml(target.author_name)}', this)" title="立即检测一次该博主的新视频（不受轮询间隔限制）" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">监测</button>
                        <button class="btn btn-secondary" onclick="startBackfill('${target.username}', '${escapeHtml(target.author_name)}')" title="翻页拉取该作者全部历史视频并加入下载队列" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">下载全部</button>
                        <button class="btn btn-secondary" onclick="editRadarTarget('${target.id}')" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">编辑</button>
                        <button class="btn btn-secondary" onclick="showRadarLogs('${target.id}', '${escapeHtml(target.author_name)}')" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">详情</button>
                        <button class="btn btn-danger" onclick="deleteRadarTarget('${target.id}')" style="padding: 4px 8px; font-size: 13px; flex-shrink: 0;">删除</button>
                    </div>
                </td>
            </tr>
        `;
    }).join('');
}

// 打开添加模态框
function openAddRadarModal() {
    document.getElementById('radarId').value = '';
    document.getElementById('radarAuthorName').value = '';
    document.getElementById('radarUsername').value = '';
    document.getElementById('radarInterval').value = 60;
    const keywordInput = document.getElementById('radarSearchKeyword');
    if (keywordInput) keywordInput.value = '';
    const results = document.getElementById('radarSearchResults');
    if (results) {
        results.style.display = 'none';
        results.innerHTML = '';
    }
    const backfillGroup = document.getElementById('radarBackfillGroup');
    if (backfillGroup) backfillGroup.style.display = 'block';

    document.getElementById('radarDialogTitle').innerText = '添加监控目标';
    const overlay = document.getElementById('addRadarDialogOverlay');
    overlay.style.display = 'flex';
    // Use setTimeout to allow display:flex to apply before adding class for transition
    setTimeout(() => overlay.classList.add('active'), 10);
}

// 打开编辑模态框
function editRadarTarget(id) {
    const target = radarTargets.find(t => t.id === id);
    if (!target) return;

    document.getElementById('radarId').value = target.id;
    document.getElementById('radarAuthorName').value = target.author_name;
    document.getElementById('radarUsername').value = target.username;
    document.getElementById('radarInterval').value = target.interval_minutes;
    const keywordInput = document.getElementById('radarSearchKeyword');
    if (keywordInput) keywordInput.value = '';
    const results = document.getElementById('radarSearchResults');
    if (results) {
        results.style.display = 'none';
        results.innerHTML = '';
    }
    // 编辑模式隐藏全量下载选项（用列表行的"下载全部"按钮触发）
    const backfillGroup = document.getElementById('radarBackfillGroup');
    if (backfillGroup) backfillGroup.style.display = 'none';

    document.getElementById('radarDialogTitle').innerText = '编辑监控目标';
    const overlay = document.getElementById('addRadarDialogOverlay');
    overlay.style.display = 'flex';
    setTimeout(() => overlay.classList.add('active'), 10);
}

// 关闭模态框
function closeAddRadarModal(event) {
    if (event) event.preventDefault();
    const overlay = document.getElementById('addRadarDialogOverlay');
    overlay.classList.remove('active');
    setTimeout(() => {
        overlay.style.display = 'none';
    }, 300); // 匹配 CSS 的 transition 时间
}

// 保存监控目标
async function saveRadarTarget() {
    const id = document.getElementById('radarId').value;
    const authorName = document.getElementById('radarAuthorName').value.trim();
    const username = document.getElementById('radarUsername').value.trim();
    const intervalMinutes = parseInt(document.getElementById('radarInterval').value, 10);

    if (!authorName) return showMessage('请输入博主名称', 'warning');
    if (!username) return showMessage('请输入视频号ID', 'warning');
    if (intervalMinutes < 5) return showMessage('监控频率不能低于5分钟', 'warning');

    const data = {
        author_name: authorName,
        username: username,
        interval_minutes: intervalMinutes
    };

    try {
        let url = '/api/v1/radar/targets';
        let method = 'POST';

        if (id) {
            url += `/${id}`;
            method = 'PUT';
        }

        const response = await fetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        });

        const res = await response.json();
        if (res.code === 0 || res.code === 200) {
            showMessage(id ? '更新成功' : '添加成功', 'success');
            closeAddRadarModal();
            loadRadarTargets();

            // 新增目标且勾选了全量下载时，启动作者全量历史视频下载
            if (!id && document.getElementById('radarBackfillEnabled').checked) {
                startBackfill(username, authorName);
            }
        } else {
            showMessage(res.message || '操作失败', 'error');
        }
    } catch (err) {
        console.error('保存监控目标失败:', err);
        showMessage('保存失败，请检查网络', 'error');
    }
}

// ------------------- 作者全量下载相关 -------------------

// 按昵称搜索账号，选中后自动填充昵称与 username
async function searchAuthorByNickname() {
    const keyword = document.getElementById('radarSearchKeyword').value.trim();
    if (!keyword) return showMessage('请输入要搜索的昵称', 'warning');

    const resultsBox = document.getElementById('radarSearchResults');
    resultsBox.style.display = 'block';
    resultsBox.innerHTML = '<div style="padding: 12px; text-align: center; color: var(--text-muted); font-size: 13px;">搜索中...</div>';

    try {
        const response = await fetch('/api/search/contact?type=1&page=1&page_size=20&keyword=' + encodeURIComponent(keyword));
        const json = await response.json();
        if (!(json.code === 0 || json.code === 200)) {
            resultsBox.innerHTML = '<div style="padding: 12px; color: var(--danger-color); font-size: 13px;">' + escapeHtml(json.message || '搜索失败') + '</div>';
            return;
        }

        const contacts = extractAuthorCandidates(json.data);
        if (contacts.length === 0) {
            resultsBox.innerHTML = '<div style="padding: 12px; color: var(--text-muted); font-size: 13px;">未找到相关账号（需要微信页面在线，若未连接请先打开一个视频号页面）</div>';
            return;
        }

        resultsBox.innerHTML = contacts.map((c, idx) => `
            <div style="padding: 8px 12px; cursor: pointer; border-bottom: 1px solid var(--border-color); font-size: 13px;"
                onmouseover="this.style.background='var(--bg-secondary)'" onmouseout="this.style.background=''"
                onclick="pickAuthorCandidate(${idx})">
                <div style="font-weight: 500;">${escapeHtml(c.name)}</div>
                <div style="color: var(--text-muted); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(c.username)}">${escapeHtml(c.username)}</div>
            </div>
        `).join('');
        window.__radarAuthorCandidates = contacts;
    } catch (err) {
        console.error('昵称搜索失败:', err);
        resultsBox.innerHTML = '<div style="padding: 12px; color: var(--danger-color); font-size: 13px;">搜索请求失败，请检查网络</div>';
    }
}

// 从账号搜索的原始微信返回中递归提取候选账号（防御性解析，兼容不同返回结构）
function extractAuthorCandidates(data) {
    const seen = new Set();
    const results = [];
    const rawUsername = (u) => (typeof u === 'string' && /^v2_[A-Za-z0-9_+\-=/@.]+$/.test(u)) ? u : '';

    function walk(node, depth) {
        if (!node || depth > 8 || results.length >= 20) return;
        if (Array.isArray(node)) {
            node.forEach(item => walk(item, depth + 1));
            return;
        }
        if (typeof node !== 'object') return;
        const username = rawUsername(node.username) || rawUsername(node.userName) || rawUsername(node.finderUserName);
        const name = node.nickname || node.nickName || node.name || '';
        if (username && name && !seen.has(username)) {
            seen.add(username);
            results.push({ name, username });
        }
        Object.values(node).forEach(v => {
            if (v && typeof v === 'object') walk(v, depth + 1);
        });
    }
    walk(data, 0);
    return results;
}

// 点选搜索结果，回填昵称与 username
function pickAuthorCandidate(idx) {
    const candidate = (window.__radarAuthorCandidates || [])[idx];
    if (!candidate) return;
    document.getElementById('radarAuthorName').value = candidate.name;
    document.getElementById('radarUsername').value = candidate.username;
    const resultsBox = document.getElementById('radarSearchResults');
    resultsBox.style.display = 'none';
    resultsBox.innerHTML = '';
}

// 启动指定作者的全量下载
async function startBackfill(username, authorName) {
    if (!username) return showMessage('缺少视频号ID，无法全量下载', 'warning');
    try {
        const response = await fetch('/api/author/backfill/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: username, author_name: authorName })
        });
        const json = await response.json();
        if (json.code === 0 || json.code === 200) {
            showMessage(authorName ? `「${authorName}」全量下载已启动` : '全量下载已启动', 'success');
            renderBackfillStatus(json.data);
        } else {
            showMessage(json.message || '全量下载启动失败', 'error');
        }
    } catch (err) {
        console.error('启动全量下载失败:', err);
        showMessage('全量下载启动失败，请检查网络', 'error');
    }
}

// 停止进行中的全量下载
async function stopBackfill() {
    if (!window.__backfillRunningJob) return;
    try {
        const response = await fetch('/api/author/backfill/stop', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: window.__backfillRunningJob.username })
        });
        const json = await response.json();
        if (json.code === 0 || json.code === 200) {
            showMessage('已请求停止全量下载', 'success');
        } else {
            showMessage(json.message || '停止失败', 'error');
        }
    } catch (err) {
        console.error('停止全量下载失败:', err);
    }
}

// 渲染全量下载任务进度
function renderBackfillStatus(job) {
    const card = document.getElementById('backfillStatusCard');
    if (!card) return;
    const resumeBtn = document.getElementById('backfillResumeButton');
    if (!job || job.status !== 'running') {
        if (job) {
            // 任务已结束：展示结果并提供"再次拉取"按钮
            card.style.display = 'block';
            window.__backfillRunningJob = null;
            window.__backfillLastJob = job;
            document.getElementById('backfillStopButton').style.display = 'none';
            if (resumeBtn) resumeBtn.style.display = '';
            document.getElementById('backfillAuthorName').textContent = job.author_name || job.username;
            document.getElementById('backfillStatusText').textContent =
                job.status === 'completed' ? '✅ 已完成' : job.status === 'stopped' ? '⏹ 已停止' : '❌ 失败';
            document.getElementById('backfillDetailText').textContent =
                `翻页 ${job.pages_fetched} 页，发现 ${job.found_videos} 个视频，新增入队 ${job.new_videos} 个，跳过已有 ${job.skipped_videos} 个` +
                (job.last_error ? `（${job.last_error}）` : '');
            document.getElementById('backfillProgressBar').style.width = '100%';
            return;
        }
        card.style.display = 'none';
        window.__backfillRunningJob = null;
        return;
    }

    window.__backfillRunningJob = job;
    window.__backfillLastJob = job;
    card.style.display = 'block';
    document.getElementById('backfillStopButton').style.display = '';
    if (resumeBtn) resumeBtn.style.display = 'none';
    document.getElementById('backfillAuthorName').textContent = job.author_name || job.username;
    document.getElementById('backfillStatusText').textContent = '⏳ 下载中...';
    document.getElementById('backfillDetailText').textContent =
        `已翻页 ${job.pages_fetched} 页，发现 ${job.found_videos} 个视频，新增入队 ${job.new_videos} 个，跳过已有 ${job.skipped_videos} 个`;
    // 无总页数信息，用已翻页数做相对进度展示（当前配置上限为经验满值）
    const maxPages = window.__backfillSettings ? (window.__backfillSettings.max_pages || 500) : 500;
    const pct = Math.min(95, (job.pages_fetched / maxPages) * 100);
    document.getElementById('backfillProgressBar').style.width = pct + '%';
}

// 对最近一次结束的任务重新发起全量拉取（已下载的视频会自动跳过）
async function startBackfillFromJob() {
    const job = window.__backfillLastJob;
    if (!job) return showMessage('没有可继续的任务', 'warning');
    await startBackfill(job.username, job.author_name);
}

// 加载全量下载设置（翻页上限、间隔）
async function loadBackfillSettings() {
    try {
        const response = await fetch('/api/author/backfill/settings');
        if (!response.ok) return;
        const json = await response.json();
        if (!(json.code === 0 || json.code === 200)) return;
        window.__backfillSettings = json.data || {};
        const maxPagesInput = document.getElementById('backfillMaxPagesInput');
        const pageDelayInput = document.getElementById('backfillPageDelayInput');
        if (maxPagesInput && window.__backfillSettings.max_pages) {
            maxPagesInput.value = window.__backfillSettings.max_pages;
        }
        if (pageDelayInput && window.__backfillSettings.page_delay) {
            pageDelayInput.value = window.__backfillSettings.page_delay;
        }
    } catch (err) { /* 静默失败 */ }
}

// 保存全量下载设置
async function saveBackfillSettings() {
    const maxPages = parseInt(document.getElementById('backfillMaxPagesInput').value, 10);
    const pageDelay = parseInt(document.getElementById('backfillPageDelayInput').value, 10);
    if (!(maxPages >= 1 && maxPages <= 10000)) return showMessage('翻页上限需在 1-10000 之间', 'warning');
    if (!(pageDelay >= 1 && pageDelay <= 600)) return showMessage('翻页间隔需在 1-600 秒之间', 'warning');

    try {
        const response = await fetch('/api/author/backfill/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ max_pages: maxPages, page_delay: pageDelay })
        });
        const json = await response.json();
        if (json.code === 0 || json.code === 200) {
            window.__backfillSettings = json.data || {};
            showMessage(`设置已保存：翻页上限 ${json.data.max_pages} 页，间隔 ${json.data.page_delay} 秒`, 'success');
        } else {
            showMessage(json.message || '保存失败', 'error');
        }
    } catch (err) {
        console.error('保存全量下载设置失败:', err);
        showMessage('保存失败，请检查网络', 'error');
    }
}

// 拉取全量下载任务状态
async function refreshBackfillStatus() {
    try {
        const response = await fetch('/api/author/backfill/status');
        if (!response.ok) return;
        const json = await response.json();
        if (!(json.code === 0 || json.code === 200)) return;
        const jobs = json.data || [];
        const running = jobs.find(j => j.status === 'running');
        if (running) {
            renderBackfillStatus(running);
        } else if (window.__backfillRunningJob) {
            // 之前的任务结束了，展示其最终状态
            const finished = jobs.find(j => j.username === window.__backfillRunningJob.username) || jobs[0];
            if (finished) renderBackfillStatus(finished);
        }
    } catch (err) { /* 静默失败，不影响页面其他功能 */ }
}

// 手动触发一次立即检测（不受轮询间隔限制）
async function checkRadarNow(id, authorName, btn) {
    const originalText = btn ? btn.textContent : '';
    if (btn) {
        btn.disabled = true;
        btn.textContent = '检测中';
    }
    try {
        const response = await fetch(`/api/v1/radar/targets/${id}/check_now`, { method: 'POST' });
        const json = await response.json();
        if (json.code === 0 || json.code === 200) {
            const log = json.data;
            if (log && log.status === 'success') {
                showMessage(`${authorName} 检测完成：发现 ${log.found_videos} 个视频，新增 ${log.new_videos} 个已入队`,
                    log.new_videos > 0 ? 'success' : 'info');
            } else if (log && log.status === 'error') {
                showMessage(`${authorName} 检测失败：${log.error_message || '未知错误'}`, 'error');
            } else {
                showMessage(`${authorName} 检测完成`, 'info');
            }
            loadRadarTargets();
        } else {
            showMessage(json.message || '检测失败', 'error');
        }
    } catch (err) {
        console.error('手动检测失败:', err);
        showMessage('检测请求失败，请检查网络', 'error');
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.textContent = originalText;
        }
    }
}

// 切换监控状态
async function toggleRadarStatus(id, newStatus) {
    try {
        const response = await fetch(`/api/v1/radar/targets/${id}/status`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status: newStatus })
        });

        if (response.ok) {
            showMessage(newStatus === 'active' ? '监控已恢复' : '监控已暂停', 'success');
            loadRadarTargets();
        } else {
            showMessage('状态更新失败', 'error');
        }
    } catch (err) {
        console.error('更新状态失败:', err);
        showMessage('网络错误', 'error');
    }
}

// 删除监控目标
async function deleteRadarTarget(id) {
    if (!confirm('确定要删除该监控目标吗？')) return;

    try {
        const response = await fetch(`/api/v1/radar/targets/${id}`, {
            method: 'DELETE'
        });

        if (response.ok) {
            showMessage('已删除', 'success');
            loadRadarTargets();
        } else {
            showMessage('删除失败', 'error');
        }
    } catch (err) {
        console.error('删除目标失败:', err);
        showMessage('网络错误', 'error');
    }
}

// ------------------- 雷达日志相关 -------------------

function closeRadarLogsModal() {
    const overlay = document.getElementById('radarLogsDialogOverlay');
    overlay.classList.remove('active');
    setTimeout(() => overlay.style.display = 'none', 300);
}

async function showRadarLogs(id, authorName) {
    document.getElementById('radarLogsTitle').innerText = `${authorName} 的监控日志`;

    const tbody = document.getElementById('radarLogsTableBody');
    tbody.innerHTML = `
        <tr>
            <td colspan="4" class="empty-state">
                <div class="video-player-spinner" style="margin: 20px auto;"></div>
                <p>正在加载日志...</p>
            </td>
        </tr>
    `;

    const overlay = document.getElementById('radarLogsDialogOverlay');
    overlay.style.display = 'flex';
    setTimeout(() => overlay.classList.add('active'), 10);

    try {
        const res = await fetch('/api/v1/radar/targets/' + id + '/logs');
        const json = await res.json();
        if (json.code === 0 || json.code === 200) {
            const logs = json.data || [];
            if (logs.length === 0) {
                tbody.innerHTML = `<tr><td colspan="5" class="empty-state">暂无执行日志，请等待首次检测...</td></tr>`;
                return;
            }

            tbody.innerHTML = logs.map((log, idx) => {
                const timeStr = new Date(log.check_time).toLocaleString();
                const statusHtml = log.status === 'success'
                    ? `<span style="color: var(--success-color);">成功</span>`
                    : `<span style="color: var(--danger-color);" title="${escapeHtml(log.error_message)}">失败: ${escapeHtml(log.error_message)}</span>`;

                let videoListHtml = '';
                let hasVideos = false;
                if (log.video_list) {
                    try {
                        const videos = JSON.parse(log.video_list);
                        if (videos && videos.length > 0) {
                            hasVideos = true;
                            videoListHtml = `
                                <tr id="radar-vlist-${idx}" style="display:none;">
                                    <td colspan="5" style="padding: 0 12px 12px; background: var(--bg-secondary);">
                                        <div style="max-height:200px; overflow-y:auto; font-size:12px;">
                                            <table style="width:100%; border-collapse:collapse;">
                                                <thead><tr style="color:var(--text-muted);">
                                                    <th style="padding:4px 8px; text-align:left; font-weight:500;">视频标题</th>
                                                    <th style="padding:4px 8px; width:70px; text-align:center; font-weight:500;">状态</th>
                                                </tr></thead>
                                                <tbody>${videos.map(v => `
                                                <tr style="border-top:1px solid var(--border-color);">
                                                    <td style="padding:4px 8px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:350px;" title="${escapeHtml(v.title)}">${escapeHtml(v.title)}</td>
                                                    <td style="padding:4px 8px; text-align:center;">${v.is_new ? '<span style="color:var(--success-color); font-weight:bold;">🆕新增</span>' : '<span style="color:var(--text-muted);">已有</span>'}</td>
                                                </tr>`).join('')}</tbody>
                                            </table>
                                        </div>
                                    </td>
                                </tr>`;
                        }
                    } catch (e) { }
                }

                return `
                    <tr style="cursor:${hasVideos ? 'pointer' : 'default'};" onclick="${hasVideos ? `toggleRadarVideoList(${idx})` : ''}">
                        <td style="color: var(--text-muted);">${timeStr}</td>
                        <td>${log.found_videos}</td>
                        <td style="color: ${log.new_videos > 0 ? 'var(--success-color)' : 'inherit'}; font-weight: ${log.new_videos > 0 ? 'bold' : 'normal'};">${log.new_videos}</td>
                        <td>${statusHtml}</td>
                        <td style="text-align:center; color:var(--text-muted);">${hasVideos ? '▶' : ''}</td>
                    </tr>
                    ${videoListHtml}`;
            }).join('');
        } else {
            tbody.innerHTML = `<tr><td colspan="5" class="empty-state" style="color:red">加载失败: ${escapeHtml(json.message)}</td></tr>`;
        }
    } catch (error) {
        tbody.innerHTML = `<tr><td colspan="5" class="empty-state" style="color:red">请求异常: ${error.message}</td></tr>`;
    }
}

function toggleRadarVideoList(idx) {
    const row = document.getElementById('radar-vlist-' + idx);
    if (!row) return;
    row.style.display = row.style.display === 'none' ? 'table-row' : 'none';
}

