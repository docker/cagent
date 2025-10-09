## Supported Remote MCP Servers

Below is a list of remote MCP servers with OAuth that work with cagent,
organized by category:

### Project Management & Collaboration

| Service    | URL                                | Transport | Description                           |
| ---------- | ---------------------------------- | --------- | ------------------------------------- |
| Asana      | `https://mcp.asana.com/sse`        | sse       | Task and project management           |
| Atlassian  | `https://mcp.atlassian.com/v1/sse` | sse       | Jira, Confluence integration          |
| Linear     | `https://mcp.linear.app/sse`       | sse       | Issue tracking and project management |
| Monday.com | `https://mcp.monday.com/sse`       | sse       | Work management platform              |
| Intercom   | `https://mcp.intercom.com/sse`     | sse       | Customer communication platform       |

### Development & Infrastructure

| Service                  | URL                                            | Transport  | Description                       |
| ------------------------ | ---------------------------------------------- | ---------- | --------------------------------- |
| GitHub                   | `https://api.githubcopilot.com/mcp`            | sse        | Version control and collaboration |
| Buildkite                | `https://mcp.buildkite.com/mcp`                | streamable | CI/CD platform                    |
| Netlify                  | `https://netlify-mcp.netlify.app/mcp`          | streamable | Web hosting and deployment        |
| Vercel                   | `https://mcp.vercel.com/`                      | sse        | Web deployment platform           |
| Cloudflare Bindings      | `https://bindings.mcp.cloudflare.com/sse`      | sse        | Edge computing resources          |
| Cloudflare Observability | `https://observability.mcp.cloudflare.com/sse` | sse        | Monitoring and analytics          |
| Grafbase                 | `https://api.grafbase.com/mcp`                 | streamable | GraphQL backend platform          |
| Neon                     | `https://mcp.neon.tech/sse`                    | sse        | Serverless Postgres database      |
| Prisma                   | `https://mcp.prisma.io/mcp`                    | streamable | Database ORM and toolkit          |
| Sentry                   | `https://mcp.sentry.dev/sse`                   | sse        | Error tracking and monitoring     |

### Content & Media

| Service    | URL                                               | Transport  | Description                       |
| ---------- | ------------------------------------------------- | ---------- | --------------------------------- |
| Canva      | `https://mcp.canva.com/mcp`                       | streamable | Design and graphics platform      |
| Cloudinary | `https://asset-management.mcp.cloudinary.com/sse` | sse        | Media management and optimization |
| InVideo    | `https://mcp.invideo.io/sse`                      | sse        | Video creation platform           |
| Webflow    | `https://mcp.webflow.com/sse`                     | sse        | Website builder and CMS           |
| Wix        | `https://mcp.wix.com/sse`                         | sse        | Website builder platform          |
| Notion     | `https://mcp.notion.com/sse`                      | sse        | Documentation and knowledge base  |

### Communication & Voice

| Service     | URL                                 | Transport  | Description                 |
| ----------- | ----------------------------------- | ---------- | --------------------------- |
| Fireflies   | `https://api.fireflies.ai/mcp`      | streamable | Meeting transcription       |
| Listenetic  | `https://mcp.listenetic.com/v1/mcp` | streamable | Audio intelligence platform |
| Carbonvoice | `https://mcp.carbonvoice.app`       | sse        | Voice communication tools   |
| Telnyx      | `https://api.telnyx.com/v2/mcp`     | streamable | Communications platform     |
| Dialer      | `https://getdialer.app/sse`         | sse        | Phone communication tools   |

### Storage & File Management

| Service | URL                                 | Transport | Description              |
| ------- | ----------------------------------- | --------- | ------------------------ |
| Box     | `https://mcp.box.com`               | sse       | Cloud content management |
| Egnyte  | `https://mcp-server.egnyte.com/sse` | sse       | Enterprise file sharing  |

### Business & Finance

| Service       | URL                                       | Transport  | Description                |
| ------------- | ----------------------------------------- | ---------- | -------------------------- |
| PayPal        | `https://mcp.paypal.com/sse`              | sse        | Payment processing         |
| Plaid         | `https://api.dashboard.plaid.com/mcp/sse` | sse        | Financial data integration |
| Square        | `https://mcp.squareup.com/sse`            | sse        | Payment processing         |
| Stytch        | `http://mcp.stytch.dev/mcp`               | streamable | Authentication platform    |
| Close         | `https://mcp.close.com/mcp`               | streamable | CRM platform               |
| Dodo Payments | `https://mcp.dodopayments.com/sse`        | sse        | Payment processing         |

### Analytics & Data

| Service     | URL                                     | Transport  | Description                    |
| ----------- | --------------------------------------- | ---------- | ------------------------------ |
| ThoughtSpot | `https://agent.thoughtspot.app/mcp`     | streamable | Analytics and BI platform      |
| Meta Ads    | `https://mcp.pipeboard.co/meta-ads-mcp` | streamable | Facebook advertising analytics |

### Utilities & Tools

| Service          | URL                                  | Transport  | Description                     |
| ---------------- | ------------------------------------ | ---------- | ------------------------------- |
| Apify            | `https://mcp.apify.com`              | sse        | Web scraping and automation     |
| AudioScrape      | `https://mcp.audioscrape.com`        | sse        | Audio data extraction           |
| SimpleScraper    | `https://mcp.simplescraper.io/mcp`   | streamable | Web scraping tool               |
| GlobalPing       | `https://mcp.globalping.dev/sse`     | sse        | Network diagnostics             |
| Jam              | `https://mcp.jam.dev/mcp`            | streamable | Bug reporting and collaboration |
| Octagon Agents   | `https://mcp.octagonagents.com/mcp`  | streamable | Agent orchestration             |
| Rube             | `https://rube.app/mcp`               | streamable | Workflow automation             |
| Turkish Tech Lab | `https://mcp.turkishtechlab.com/mcp` | streamable | Technology tools                |
| Waystation       | `https://waystation.ai/mcp`          | streamable | AI infrastructure               |
| Zenable          | `https://mcp.www.zenable.app/`       | sse        | Productivity tools              |
| Zine             | `https://www.zine.ai/mcp`            | streamable | Content creation                |

### Specialized Services

| Service           | URL                                              | Transport  | Description                    |
| ----------------- | ------------------------------------------------ | ---------- | ------------------------------ |
| Hive Intelligence | `https://hiveintelligence.xyz/mcp`               | streamable | AI intelligence platform       |
| The Kollektiv     | `https://mcp.thekollektiv.ai/sse`                | sse        | AI collaboration tools         |
| RAG MCP           | `https://rag-mcp-2.whatsmcp.workers.dev/sse`     | sse        | Retrieval-augmented generation |
| Scorecard         | `https://scorecard-mcp.dare-d5b.workers.dev/sse` | sse        | Performance tracking           |
