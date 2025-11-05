// API Module for Cagent Chat
const API = {
    // Get default headers
    getHeaders() {
        const headers = {
            'Content-Type': 'application/json'
        };
        
        const token = Auth.getToken();
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
        }
        
        return headers;
    },
    
    // Handle API response
    async handleResponse(response) {
        if (response.status === 401) {
            Auth.logout();
            throw new Error('Unauthorized');
        }
        
        if (!response.ok) {
            const error = await response.json().catch(() => ({ error: 'Request failed' }));
            throw new Error(error.error || error.message || 'Request failed');
        }
        
        return response;
    },
    
    // Get all agents
    async getAgents() {
        const response = await fetch(Config.getApiUrl('/api/agents'), {
            headers: this.getHeaders()
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Get sessions for a specific agent
    async getSessionsByAgent(agentId) {
        const response = await fetch(Config.getApiUrl(`/api/sessions/agent/${agentId}`), {
            headers: this.getHeaders()
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Get all sessions
    async getAllSessions() {
        const response = await fetch(Config.getApiUrl('/api/sessions'), {
            headers: this.getHeaders()
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Get a specific session
    async getSession(sessionId, limit = 50, before = null) {
        let url = Config.getApiUrl(`/api/sessions/${sessionId}?limit=${limit}`);
        if (before) {
            url += `&before=${before}`;
        }
        
        const response = await fetch(url, {
            headers: this.getHeaders()
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Create a new session
    async createSession(workingDir = '') {
        const response = await fetch(Config.getApiUrl('/api/sessions'), {
            method: 'POST',
            headers: this.getHeaders(),
            body: JSON.stringify({
                working_dir: workingDir,
                tools_approved: false,
                max_iterations: 0
            })
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Delete a session
    async deleteSession(sessionId) {
        const response = await fetch(Config.getApiUrl(`/api/sessions/${sessionId}`), {
            method: 'DELETE',
            headers: this.getHeaders()
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Resume a session
    async resumeSession(sessionId, confirmation) {
        const response = await fetch(Config.getApiUrl(`/api/sessions/${sessionId}/resume`), {
            method: 'POST',
            headers: this.getHeaders(),
            body: JSON.stringify({ confirmation })
        });
        
        await this.handleResponse(response);
        return response.json();
    },
    
    // Send message and stream response
    streamMessage(sessionId, agentId, messages) {
        const url = Config.getApiUrl(`/api/sessions/${sessionId}/agent/${agentId}`);
        const token = Auth.getToken();
        
        return new EventSource(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': token ? `Bearer ${token}` : ''
            },
            body: JSON.stringify(messages)
        });
    },
    
    // Send message with fetch for SSE streaming
    async sendMessageWithStreaming(sessionId, agentId, messages, onChunk, onError) {
        const url = Config.getApiUrl(`/api/sessions/${sessionId}/agent/${agentId}`);
        const headers = this.getHeaders();
        
        try {
            const response = await fetch(url, {
                method: 'POST',
                headers: headers,
                body: JSON.stringify(messages)
            });
            
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            
            while (true) {
                const { done, value } = await reader.read();
                
                if (done) break;
                
                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';
                
                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        const dataStr = line.slice(6);
                        if (dataStr.trim()) {
                            try {
                                const data = JSON.parse(dataStr);
                                onChunk(data);
                            } catch (e) {
                                console.error('Failed to parse SSE data:', e);
                            }
                        }
                    }
                }
            }
        } catch (error) {
            console.error('Streaming error:', error);
            if (onError) {
                onError(error);
            }
        }
    }
};