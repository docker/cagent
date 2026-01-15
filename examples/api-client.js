#!/usr/bin/env node

/**
 * Example script that demonstrates using the cagent API to:
 * 1. Create a session
 * 2. Ask the agent to run a tool
 * 3. Stream and print the agent's response
 *
 * Usage:
 *   1. Start the API server: cagent api nick-bug-report.yaml
 *   2. Run this script: node api-client.js
 */

const http = require('http');

const port = 8080;
const API_BASE_URL = 'http://localhost:' + port;
const AGENT_ID = 'nick-bug-report';

/**
 * Make an HTTP request
 */
function makeRequest(options, data = null) {
  return new Promise((resolve, reject) => {
    const req = http.request(options, (res) => {
      let body = '';

      res.on('data', (chunk) => {
        body += chunk;
      });

      res.on('end', () => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          try {
            resolve(JSON.parse(body));
          } catch (e) {
            resolve(body);
          }
        } else {
          reject(new Error(`HTTP ${res.statusCode}: ${body}`));
        }
      });
    });

    req.on('error', reject);

    if (data) {
      req.write(JSON.stringify(data));
    }

    req.end();
  });
}

/**
 * Create a new session
 */
async function createSession() {
  console.log('Creating session...');

  const options = {
    hostname: 'localhost',
    port: port,
    path: '/api/sessions',
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
  };

  const sessionData = {
    title: 'Test Session',
    tools_approved: false, // Require tool approval
  };

  const session = await makeRequest(options, sessionData);
  console.log(`Session created: ${session.id}\n`);
  return session;
}

/**
 * Resume a session with tool approval
 */
async function resumeSession(sessionId, confirmation) {
  console.log(`[Resuming session with: ${confirmation}]`);

  const options = {
    hostname: 'localhost',
    port: port,
    path: `/api/sessions/${sessionId}/resume`,
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
  };

  const data = {
    confirmation: confirmation, // "approve", "approve-session", or "reject"
  };

  await makeRequest(options, data);
}

/**
 * Run the agent and stream responses
 */
function runAgent(sessionId, agentId, messages) {
  return new Promise((resolve, reject) => {
    console.log('Running agent...\n');

    const postData = JSON.stringify(messages);

    const options = {
      hostname: 'localhost',
      port: port,
      path: `/api/sessions/${sessionId}/agent/${encodeURIComponent(agentId)}`,
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(postData),
      },
    };

    const req = http.request(options, (res) => {
      if (res.statusCode !== 200) {
        let errorBody = '';
        res.on('data', (chunk) => {
          errorBody += chunk;
        });
        res.on('end', () => {
          reject(new Error(`HTTP ${res.statusCode}: ${errorBody}`));
        });
        return;
      }

      let buffer = '';

      res.on('data', (chunk) => {
        buffer += chunk.toString();

        // Process complete SSE events
        const lines = buffer.split('\n');
        buffer = lines.pop(); // Keep incomplete line in buffer

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const data = line.slice(6);
            try {
              const event = JSON.parse(data);
              handleEvent(event, sessionId);
            } catch (e) {
              console.error('Error parsing event:', e.message);
            }
          }
        }
      });

      res.on('end', () => {
        console.log('\n✓ Agent finished');
        resolve();
      });
    });

    req.on('error', reject);
    req.write(postData);
    req.end();
  });
}

/**
 * Handle different event types from the agent stream
 */
function handleEvent(event, sessionId) {
  switch (event.type) {
    case 'stream_started':
      console.log(`[Stream Started] Session: ${event.session_id}, Agent: ${event.agent_name}`);
      break;

    case 'agent_choice':
      if (event.content && event.content.trim()) {
        process.stdout.write(event.content);
      }
      break;

    case 'tool_call':
      console.log(`\n[Tool Call] ${event.tool_call.function.name}`);
      console.log(`Arguments: ${event.tool_call.function.arguments}`);
      break;

    case 'tool_call_response':
      console.log(`[Tool Response]`);
      console.log(event.result);
      break;

    case 'tool_call_confirmation':
      console.log(`[Tool Confirmation Required] ${event.tool_call.function.name}`);
      // Automatically approve the tool
      resumeSession(sessionId, 'garbage').catch(err => {
        console.error('Error approving tool:', err.message);
      });
      break;

    case 'error':
      console.error(`\n[Error] ${event.error}`);
      break;

    case 'stream_stopped':
      console.log(`\n[Stream Stopped] Reason: ${event.stop_reason}`);
      break;

    default:
      // Uncomment to see all event types:
      // console.log(`[${event.type}]`, event);
      break;
  }
}

/**
 * Main execution
 */
async function main() {
  try {
    // Step 1: Create a session
    const session = await createSession();

    // Step 2: Send a message asking the agent to run the tool
    const messages = [
      {
        role: 'user',
        content: 'Please run the start-task tool with task name "demo-task" to create a new task. What error do you see?',
      },
    ];

    // Step 3: Run the agent and stream responses
    await runAgent(session.id, AGENT_ID, messages);

  } catch (error) {
    console.error('Error:', error);
    process.exit(1);
  }
}

// Run if executed directly
if (require.main === module) {
  main();
}

module.exports = { createSession, runAgent, resumeSession };
