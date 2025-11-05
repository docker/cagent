// Chat Module
let currentSession = null;
let currentAgent = null;
let sessions = [];
let agents = [];
let isStreaming = false;

// Format agent name for display
function formatAgentName(name) {
    // Remove .yaml extension if present
    let displayName = name.replace(/\.yaml$/, '');
    
    // Convert underscores to spaces
    displayName = displayName.replace(/_/g, ' ');
    
    // Capitalize first letter of each word
    displayName = displayName.replace(/\b\w/g, char => char.toUpperCase());
    
    // Special cases for common agent names
    const nameMap = {
        'Basic Agent': 'Basic Assistant',
        'Pirate': 'Pirate Assistant',
        'Gmail Agent': 'Gmail Manager',
        'Mcp Orchestrator': 'MCP Orchestrator',
        'Travel Agent': 'Travel Planner',
        'Test Agent': 'Test Assistant'
    };
    
    return nameMap[displayName] || displayName;
}

// Load agents
async function loadAgents() {
    try {
        agents = await API.getAgents();
        const agentSelect = document.getElementById('agent-select');
        
        agentSelect.innerHTML = '<option value="">Select an agent...</option>';
        agents.forEach(agent => {
            const option = document.createElement('option');
            option.value = agent.name;  // Keep the actual name for API calls
            
            // Format the display name
            const displayName = formatAgentName(agent.name);
            option.textContent = `${displayName}${agent.multi ? ' (Multi-agent)' : ''}`;
            option.title = agent.description || `${displayName} - ${agent.name}`;
            agentSelect.appendChild(option);
        });
        
        // Restore previously selected agent if any
        const lastAgent = localStorage.getItem('last_selected_agent');
        if (lastAgent && agents.find(a => a.name === lastAgent)) {
            agentSelect.value = lastAgent;
            await onAgentChange();
        }
    } catch (error) {
        console.error('Failed to load agents:', error);
        showToast('Failed to load agents', 'error');
    }
}

// Handle agent selection change
async function onAgentChange() {
    const agentSelect = document.getElementById('agent-select');
    const selectedAgentId = agentSelect.value;
    
    if (!selectedAgentId) {
        document.getElementById('sessions-container').innerHTML = 
            '<p class="info-text">Select an agent to view sessions</p>';
        currentAgent = null;
        return;
    }
    
    currentAgent = agents.find(a => a.name === selectedAgentId);
    localStorage.setItem('last_selected_agent', selectedAgentId);
    
    // Load sessions for this agent
    await loadSessions(selectedAgentId);
}

// Load sessions for an agent
async function loadSessions(agentId) {
    try {
        sessions = await API.getSessionsByAgent(agentId);
        renderSessions();
    } catch (error) {
        console.error('Failed to load sessions:', error);
        showToast('Failed to load sessions', 'error');
    }
}

// Render sessions list
function renderSessions() {
    const container = document.getElementById('sessions-container');
    
    if (sessions.length === 0) {
        container.innerHTML = '<p class="info-text">No sessions yet. Create a new one!</p>';
        return;
    }
    
    container.innerHTML = '';
    sessions.forEach(session => {
        const sessionEl = document.createElement('div');
        sessionEl.className = 'session-item';
        if (currentSession && currentSession.id === session.id) {
            sessionEl.classList.add('active');
        }
        
        const title = session.title || `Session ${session.id.substring(0, 8)}`;
        const date = new Date(session.created_at).toLocaleDateString();
        const tokens = session.input_tokens + session.output_tokens;
        
        sessionEl.innerHTML = `
            <div class="session-item-title">${escapeHtml(title)}</div>
            <div class="session-item-info">
                ID: ${session.id}
            </div>
            <div class="session-item-info">
                ${date} • ${session.num_messages} messages • ${tokens} tokens
            </div>
        `;
        
        sessionEl.onclick = () => loadSession(session.id);
        container.appendChild(sessionEl);
    });
}

// Create new session
async function createNewSession() {
    if (!currentAgent) {
        showToast('Please select an agent first', 'error');
        return;
    }
    
    try {
        showLoading(true);
        const session = await API.createSession();
        currentSession = session;
        
        // Add to sessions list
        sessions.unshift({
            id: session.id,
            title: session.title || 'New Session',
            created_at: session.created_at,
            num_messages: 0,
            input_tokens: 0,
            output_tokens: 0
        });
        
        renderSessions();
        showChatInterface();
        showToast('New session created', 'success');
    } catch (error) {
        console.error('Failed to create session:', error);
        showToast('Failed to create session', 'error');
    } finally {
        showLoading(false);
    }
}

// Load a session
async function loadSession(sessionId) {
    try {
        showLoading(true);
        currentSession = await API.getSession(sessionId);
        showChatInterface();
        renderMessages();
        
        // Update sessions list to show active session
        renderSessions();
    } catch (error) {
        console.error('Failed to load session:', error);
        showToast('Failed to load session', 'error');
    } finally {
        showLoading(false);
    }
}

// Delete current session
async function deleteCurrentSession() {
    if (!currentSession) return;
    
    if (!confirm('Are you sure you want to delete this session? This cannot be undone.')) {
        return;
    }
    
    try {
        showLoading(true);
        await API.deleteSession(currentSession.id);
        
        // Remove from sessions list
        sessions = sessions.filter(s => s.id !== currentSession.id);
        currentSession = null;
        
        renderSessions();
        hideChatInterface();
        showToast('Session deleted', 'success');
    } catch (error) {
        console.error('Failed to delete session:', error);
        showToast('Failed to delete session', 'error');
    } finally {
        showLoading(false);
    }
}

// Show chat interface
function showChatInterface() {
    document.getElementById('welcome-screen').style.display = 'none';
    document.getElementById('chat-interface').style.display = 'flex';
    
    // Update session info
    const title = currentSession.title || `Session ${currentSession.id.substring(0, 8)}`;
    document.getElementById('session-title').textContent = `${title} (${currentSession.id})`;
    
    const tokens = currentSession.input_tokens + currentSession.output_tokens;
    document.getElementById('token-count').textContent = `Tokens: ${tokens}`;
}

// Hide chat interface
function hideChatInterface() {
    document.getElementById('welcome-screen').style.display = 'flex';
    document.getElementById('chat-interface').style.display = 'none';
}

// Render messages
function renderMessages() {
    const container = document.getElementById('messages-list');
    container.innerHTML = '';
    
    if (!currentSession || !currentSession.messages) return;
    
    currentSession.messages.forEach(msg => {
        addMessageToUI(msg);
    });
    
    // Scroll to bottom
    const messagesContainer = document.getElementById('messages-container');
    messagesContainer.scrollTop = messagesContainer.scrollHeight;
}

// Add message to UI
function addMessageToUI(message) {
    const container = document.getElementById('messages-list');
    const messageEl = document.createElement('div');
    
    // Handle nested message structure from API
    // The API returns messages with structure: { message: { role, content }, agentFilename, agentName }
    let role, content;
    if (message.message) {
        // API response format - nested structure
        role = message.message.role;
        content = message.message.content;
    } else {
        // Direct format (for local messages)
        role = message.role;
        content = message.content;
    }
    
    messageEl.className = `message message-${role}`;
    
    const avatar = document.createElement('div');
    avatar.className = 'message-avatar';
    avatar.textContent = role === 'user' ? 'U' : 'A';
    
    const contentEl = document.createElement('div');
    contentEl.className = 'message-content';
    contentEl.innerHTML = formatMessageContent(content);
    
    messageEl.appendChild(avatar);
    messageEl.appendChild(contentEl);
    container.appendChild(messageEl);
}

// Format message content (handle markdown, code blocks, etc.)
function formatMessageContent(content) {
    if (!content) return '';
    
    // Escape HTML first
    let formatted = escapeHtml(content);
    
    // Convert code blocks
    formatted = formatted.replace(/```(\w+)?\n([\s\S]*?)```/g, (match, lang, code) => {
        return `<pre><code class="${lang || ''}">${code}</code></pre>`;
    });
    
    // Convert inline code
    formatted = formatted.replace(/`([^`]+)`/g, '<code>$1</code>');
    
    // Convert bold
    formatted = formatted.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    
    // Convert italic
    formatted = formatted.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    
    // Convert line breaks
    formatted = formatted.replace(/\n/g, '<br>');
    
    return formatted;
}

// Send message
async function sendMessage() {
    const input = document.getElementById('message-input');
    const messageText = input.value.trim();
    
    if (!messageText || !currentSession || !currentAgent || isStreaming) return;
    
    // Clear input
    input.value = '';
    
    // Add user message to UI
    const userMessage = {
        role: 'user',
        content: messageText
    };
    addMessageToUI(userMessage);
    
    // Show typing indicator
    showTypingIndicator(true);
    
    // Disable send button
    const sendButton = document.getElementById('send-button');
    sendButton.disabled = true;
    isStreaming = true;
    
    // Prepare message for API
    const messages = [{
        role: 'user',
        content: messageText
    }];
    
    // Stream response
    let assistantMessage = {
        role: 'assistant',
        content: ''
    };
    let messageAdded = false;
    
    try {
        await API.sendMessageWithStreaming(
            currentSession.id,
            currentAgent.name,
            messages,
            (chunk) => {
                // Handle streaming chunk
                if (chunk.type === 'message') {
                    if (!messageAdded) {
                        showTypingIndicator(false);
                        addMessageToUI(assistantMessage);
                        messageAdded = true;
                    }
                    
                    assistantMessage.content += chunk.content || '';
                    updateLastMessage(assistantMessage.content);
                } else if (chunk.type === 'token_usage') {
                    // Update token count
                    currentSession.input_tokens += chunk.input_tokens || 0;
                    currentSession.output_tokens += chunk.output_tokens || 0;
                    const tokens = currentSession.input_tokens + currentSession.output_tokens;
                    document.getElementById('token-count').textContent = `Tokens: ${tokens}`;
                } else if (chunk.type === 'error') {
                    showToast(chunk.message || 'An error occurred', 'error');
                }
                
                // Auto-scroll
                const messagesContainer = document.getElementById('messages-container');
                messagesContainer.scrollTop = messagesContainer.scrollHeight;
            },
            (error) => {
                console.error('Streaming error:', error);
                showToast('Failed to send message', 'error');
                showTypingIndicator(false);
            }
        );
        
        // Update session in the background
        loadSessions(currentAgent.name);
        
    } catch (error) {
        console.error('Failed to send message:', error);
        showToast('Failed to send message', 'error');
    } finally {
        showTypingIndicator(false);
        sendButton.disabled = false;
        isStreaming = false;
    }
}

// Update last message content
function updateLastMessage(content) {
    const messages = document.querySelectorAll('.message-assistant');
    if (messages.length > 0) {
        const lastMessage = messages[messages.length - 1];
        const contentEl = lastMessage.querySelector('.message-content');
        if (contentEl) {
            contentEl.innerHTML = formatMessageContent(content);
        }
    }
}

// Show/hide typing indicator
function showTypingIndicator(show) {
    const indicator = document.getElementById('typing-indicator');
    indicator.style.display = show ? 'flex' : 'none';
    
    if (show) {
        const messagesContainer = document.getElementById('messages-container');
        messagesContainer.scrollTop = messagesContainer.scrollHeight;
    }
}

// Handle input keydown
function handleInputKeydown(event) {
    if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        sendMessage();
    }
}

// Escape HTML
function escapeHtml(text) {
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return text.replace(/[&<>"']/g, m => map[m]);
}