const form = document.getElementById('research-form');
const input = document.getElementById('question-input');
const submitBtn = document.getElementById('submit-btn');
const traceList = document.getElementById('trace-list');
const answerBody = document.getElementById('answer-body');

form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const question = input.value.trim();
    if (!question) return;

    submitBtn.disabled = true;
    submitBtn.textContent = 'Researching';

    // Reset both panels.
    traceList.innerHTML = '';
    answerBody.innerHTML = '<p class="placeholder loading-dots">Working</p>';

    try {
        const resp = await fetch('/research', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ question }),
        });

        if (!resp.ok) {
            const text = await resp.text();
            throw new Error(`HTTP ${resp.status}: ${text}`);
        }

        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop();  // hold the incomplete tail

            for (const line of lines) {
                if (!line.trim()) continue;
                try {
                    handleEvent(JSON.parse(line));
                } catch (err) {
                    console.warn('bad event', line, err);
                }
            }
        }
    } catch (err) {
        addTrace(`error: ${err.message}`, true);
        answerBody.innerHTML = `<p class="placeholder">Error: ${escapeHTML(err.message)}</p>`;
    } finally {
        submitBtn.disabled = false;
        submitBtn.textContent = 'Research';
    }
});

function handleEvent(event) {
    switch (event.event) {
        case 'trace':
            addTrace(event.message);
            break;
        case 'error':
            addTrace(event.message, true);
            break;
        case 'answer':
            renderAnswer(event.answer);
            break;
        default:
            console.warn('unknown event', event);
    }
}

function addTrace(message, isError = false) {
    const li = document.createElement('li');
    if (isError) li.className = 'error';
    li.textContent = message;
    traceList.appendChild(li);
    li.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

function renderAnswer(markdown) {
    if (typeof marked === 'undefined') {
        answerBody.textContent = markdown;
    } else {
        answerBody.innerHTML = marked.parse(markdown);
    }
}

function escapeHTML(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}
