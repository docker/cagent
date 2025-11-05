// Chat Module
let currentSession = null;
let currentAgent = null;
let sessions = [];
let agents = [];
let isStreaming = false;
let pendingApproval = null;  // Store pending approval request

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
            <div class="session-item-content" onclick="loadSession('${session.id}')">
                <div class="session-item-title">${escapeHtml(title)}</div>
                <div class="session-item-info">
                    ID: ${session.id}
                </div>
                <div class="session-item-info">
                    ${date} • ${session.num_messages} messages • ${tokens} tokens
                </div>
            </div>
            <button class="session-delete-btn" onclick="event.stopPropagation(); deleteSession('${session.id}')" title="Delete session">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    <line x1="10" y1="11" x2="10" y2="17"></line>
                    <line x1="14" y1="11" x2="14" y2="17"></line>
                </svg>
            </button>
        `;
        
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
        handleMobileMenuClose();  // Close mobile menu
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
        handleMobileMenuClose();  // Close mobile menu
        
        // Update sessions list to show active session
        renderSessions();
    } catch (error) {
        console.error('Failed to load session:', error);
        showToast('Failed to load session', 'error');
    } finally {
        showLoading(false);
    }
}

// Delete any session from the list
async function deleteSession(sessionId) {
    const sessionToDelete = sessions.find(s => s.id === sessionId);
    if (!sessionToDelete) return;
    
    const title = sessionToDelete.title || `Session ${sessionId.substring(0, 8)}`;
    if (!confirm(`Delete session "${title}"?\n\nThis action cannot be undone.`)) {
        return;
    }
    
    try {
        showLoading(true);
        await API.deleteSession(sessionId);
        
        // Remove from sessions list
        sessions = sessions.filter(s => s.id !== sessionId);
        
        // If deleted the current session, clear the chat interface
        if (currentSession && currentSession.id === sessionId) {
            currentSession = null;
            hideChatInterface();
        }
        
        renderSessions();
        showToast('Session deleted', 'success');
    } catch (error) {
        console.error('Failed to delete session:', error);
        showToast('Failed to delete session', 'error');
    } finally {
        showLoading(false);
    }
}

// Delete current session (for the button in chat interface)
async function deleteCurrentSession() {
    if (!currentSession) return;
    await deleteSession(currentSession.id);
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
                } else if (chunk.type === 'agent_choice') {
                    // Handle agent response content
                    if (!messageAdded) {
                        showTypingIndicator(false);
                        addMessageToUI(assistantMessage);
                        messageAdded = true;
                    }
                    
                    assistantMessage.content += chunk.content || '';
                    updateLastMessage(assistantMessage.content);
                } else if (chunk.type === 'tool_call') {
                    // Show tool being called - handle different field names
                    const toolName = chunk.tool_call?.name || chunk.name || chunk.tool || 'unknown';
                    const toolArgs = chunk.tool_call?.arguments || chunk.arguments || {};
                    const toolInfo = `\n🔧 Using tool: ${toolName}\n`;
                    if (!messageAdded) {
                        showTypingIndicator(false);
                        assistantMessage.content = toolInfo;
                        addMessageToUI(assistantMessage);
                        messageAdded = true;
                    } else {
                        assistantMessage.content += toolInfo;
                        updateLastMessage(assistantMessage.content);
                    }
                } else if (chunk.type === 'tool_response') {
                    // Show tool response
                    const toolResult = `✓ Tool completed\n`;
                    assistantMessage.content += toolResult;
                    updateLastMessage(assistantMessage.content);
                } else if (chunk.type === 'approval_required' || chunk.type === 'tool_approval_required') {
                    // Handle approval request
                    showTypingIndicator(false);
                    
                    // Extract tool name and arguments
                    const toolName = chunk.tool_name || chunk.tool || chunk.name || 'unknown';
                    const toolArgs = chunk.arguments || chunk.args || {};
                    
                    // Store pending approval
                    pendingApproval = {
                        sessionId: currentSession.id,
                        toolName: toolName,
                        arguments: toolArgs
                    };
                    
                    // Show approval dialog
                    showApprovalDialog(toolName, toolArgs);
                    
                    if (!messageAdded) {
                        assistantMessage.content = `⚠️ Tool approval required: ${toolName}\n`;
                        addMessageToUI(assistantMessage);
                        messageAdded = true;
                    } else {
                        assistantMessage.content += `\n⚠️ Tool approval required: ${toolName}\n`;
                        updateLastMessage(assistantMessage.content);
                    }
                } else if (chunk.type === 'agent_choice_reasoning') {
                    // Show reasoning if available
                    if (chunk.content && !messageAdded) {
                        showTypingIndicator(false);
                        assistantMessage.content = `💭 Thinking: ${chunk.content}\n\n`;
                        addMessageToUI(assistantMessage);
                        messageAdded = true;
                    }
                } else if (chunk.type === 'token_usage') {
                    // Update token count
                    currentSession.input_tokens += chunk.input_tokens || 0;
                    currentSession.output_tokens += chunk.output_tokens || 0;
                    const tokens = currentSession.input_tokens + currentSession.output_tokens;
                    document.getElementById('token-count').textContent = `Tokens: ${tokens}`;
                } else if (chunk.type === 'error') {
                    showToast(chunk.message || 'An error occurred', 'error');
                    if (!messageAdded) {
                        showTypingIndicator(false);
                        assistantMessage.content = `❌ Error: ${chunk.message || 'An error occurred'}`;
                        addMessageToUI(assistantMessage);
                        messageAdded = true;
                    }
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

// Show approval dialog
function showApprovalDialog(toolName, args) {
    // Create approval dialog element
    const dialog = document.createElement('div');
    dialog.id = 'approval-dialog';
    dialog.className = 'approval-dialog';
    
    const argsDisplay = Object.entries(args || {})
        .map(([k, v]) => `<li><strong>${escapeHtml(k)}:</strong> ${escapeHtml(String(v).substring(0, 100))}...</li>`)
        .join('');
    
    dialog.innerHTML = `
        <div class="approval-dialog-content">
            <h3>Tool Approval Required</h3>
            <p>The agent wants to use the following tool:</p>
            <div class="tool-info">
                <strong>Tool:</strong> ${escapeHtml(toolName)}<br>
                ${argsDisplay ? `<strong>Arguments:</strong><ul>${argsDisplay}</ul>` : ''}
            </div>
            <div class="approval-buttons">
                <button onclick="handleApproval('yes')" class="btn-primary">Approve Once</button>
                <button onclick="handleApproval('always')" class="btn-secondary">Always Allow</button>
                <button onclick="handleApproval('no')" class="btn-danger">Deny</button>
            </div>
        </div>
    `;
    
    document.body.appendChild(dialog);
}

// Handle approval decision
async function handleApproval(decision) {
    const dialog = document.getElementById('approval-dialog');
    if (dialog) {
        dialog.remove();
    }
    
    if (!pendingApproval) return;
    
    const { sessionId, toolName, arguments: args } = pendingApproval;
    
    if (decision === 'yes' || decision === 'always') {
        // Send approval to API
        try {
            const response = await fetch(Config.getApiUrl(`/api/sessions/${sessionId}/approve`), {
                method: 'POST',
                headers: API.getHeaders(),
                body: JSON.stringify({
                    tool: toolName,
                    approved: true,
                    always: decision === 'always'
                })
            });
            
            if (response.ok) {
                showToast(`Tool ${decision === 'always' ? 'always allowed' : 'approved'}`, 'success');
            }
        } catch (error) {
            console.error('Failed to send approval:', error);
            showToast('Failed to send approval', 'error');
        }
    } else {
        // Send denial
        try {
            const response = await fetch(Config.getApiUrl(`/api/sessions/${sessionId}/approve`), {
                method: 'POST',
                headers: API.getHeaders(),
                body: JSON.stringify({
                    tool: toolName,
                    approved: false
                })
            });
            
            if (response.ok) {
                showToast('Tool denied', 'info');
            }
        } catch (error) {
            console.error('Failed to send denial:', error);
        }
    }
    
    pendingApproval = null;
}
