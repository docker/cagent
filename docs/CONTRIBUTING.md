# Contributing to `cagent`

## Development environment setup

> We currently support `cagent` development on macOS and Linux-based systems. Windows support coming soon

#### Build from source

If you're hacking on `cagent`, or just want to be on the bleeding edge, then building from source is a must.

##### Prerequisites

- Go 1.25 or higher
- API key(s) for your chosen AI provider (OpenAI, Anthropic, Gemini, etc.)
- [Task 3.44 or higher](https://taskfile.dev/installation/)
- [`golangci-lint`](https://golangci-lint.run/docs/welcome/install/#binaries)

> Note: On windows, we currently only support building from source via docker with `task build-local`  
>
> See [here](#building-with-docker) for more details

##### Build commands

```bash
# Clone and build
git clone https://github.com/docker/cagent.git
cd cagent
task build

# Set keys for remote inference services
export OPENAI_API_KEY=your_api_key_here    # For OpenAI models
export ANTHROPIC_API_KEY=your_api_key_here # For Anthropic models
export GOOGLE_API_KEY=your_api_key_here    # For Gemini models
export MISTRAL_API_KEY=your_api_key_here   # For Mistral models

# Run with a sample configuration
./bin/cagent run examples/code.yaml

# or specify a different agent from the config
./bin/cagent run examples/code.yaml -a root

# or run directly from an image reference
./bin/cagent run agentcatalog/pirate
```

### Building with Docker

Binary builds can also be made using `docker` itself. 

Start a build via docker using `task build-local` (for only your local architecture), or use `task cross` to build for all supported platforms.  

Builds done via `docker` will be placed in the `./dist` directory

```sh
$ task build-local
```

### 🎯 Core `cagent` Concepts

- **Root Agent**: Main entry point that coordinates the system. This represents the first agent you interact with
- **Sub-Agents**: Specialized agents for specific domains or tasks
- **Tools**: External capabilities agents can use via the Model Context Protocol (MCP)
- **Models**: Models agents can be configured to use. They include the AI provider and the model configuration (model to use, max_tokens, temperature, etc.)

### Agent <-> Sub-Agent Delegation Flow

1. User interacts with root agent
2. Root agent analyzes the request
3. Root agent can decide to delegate to appropriate sub-agent if specialized knowledge is needed
4. Sub-agent processes the task delegated to it using its tools and expertise, in its own agentic loop.
5. Results eventually flow back to the root agent and the user

## DogFooding: using `cagent` to code on `cagent`

A smart way to improve `cagent`'s codebase and feature set is to do it with the help of a `cagent` agent!

We have one that we use and that you should use too:

```sh
cd cagent
cagent run ./golang_developer.yaml
```

This agent is an *expert Golang developer specializing in the Docker `cagent` multi-agent AI system architecture*.

Ask it anything about `cagent`. It can be questions about the current code or about
improvements to the code. It can also fix issues and implement new features!

## Add a new model provider

More details on how to add a new model provider can be found in [PROVIDERS.md](/docs/PROVIDERS.md)

## Opening issues

Issues can be opened on our repo [here](https://github.com/docker/cagent/issues).  
Please use the included template when filing an Issue.

Only use Issues to report bugs in the code or to request new features. This is not a support channel for general usage of the software.

Please search for pre-existing issues which may describe the problem you are having before opening new issues. This helps us track issues and develop solutions more efficiently.

## Code style

We use `golangci-lint` to lint our project, with the rules found in [.golangci.yml](../.golangci.yml).

As long as the linter passes without errors when using `task lint`, the code is most likely _stylistically_ good enough to be merged

## Sign your work

The sign-off is a simple line at the end of the explanation for the patch. Your signature certifies that you wrote the patch or otherwise have the right to pass it on as an open-source patch. The rules are pretty simple: if you can certify the below (from developercertificate.org):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

Signed-off-by: Joe Smith <joe.smith@email.com>

Use your real name (sorry, no pseudonyms or anonymous contributions.)

If you set your `user.name` and `user.email` git configs, you can sign your commit automatically with `git commit -s`.

## How can I become a maintainer?

Since we're at the start of this project, for the moment we are not accepting external maintainers. Contributions are always accepted though.  

This decision allows us to maintain better focus and move more quickly with our ideas and the ideas shared by the community.  

As the project grows and gathers adoption in the industry, this decision will be certainly re-evaluated and more details will be added here.

## Code of conduct

We want to keep the `cagent` and `Docker` communities awesome, growing and collaborative. We need your help to keep it that way. To help with this we've come up with some general guidelines for the community as a whole:

- Be nice: Be courteous, respectful and polite to fellow community members: no regional, racial, gender, or other abuse will be tolerated. We like nice people way better than mean ones!

- Encourage diversity and participation: Make everyone in our community feel welcome, regardless of their background and the extent of their contributions, and do everything possible to encourage participation in our community.

- Keep it legal: Basically, don't get us in trouble. Share only content that you own, do not share private or sensitive information, and don't break the law.

- Stay on topic: Make sure that you are posting to the correct channel and avoid off-topic discussions. Remember when you update an issue or respond to an email you are potentially sending to a large number of people. Please consider this before you update. Also remember that nobody likes spam.

- Don't send email to the maintainers: There's no need to send email to the maintainers to ask them to investigate an issue or to take a look at a pull request. Instead of sending an email, GitHub mentions should be used to ping maintainers to review a pull request, a proposal or an issue.

The governance for this repository is handled by Docker Inc.

Guideline violations — 3 strikes method

The point of this section is not to find opportunities to punish people, but we do need a fair way to deal with people who are making our community suck.

- First occurrence: We'll give you a friendly, but public reminder that the behavior is inappropriate according to our guidelines.

- Second occurrence: We will send you a private message with a warning that any additional violations will result in removal from the community.

- Third occurrence: Depending on the violation, we may need to delete or ban your account.

Notes:

- Obvious spammers are banned on first occurrence. If we don't do this, we'll have spam all over the place.

- Violations are forgiven after 6 months of good behavior, and we won't hold a grudge.

- People who commit minor infractions will get some education, rather than hammering them in the 3 strikes process.

- The rules apply equally to everyone in the community, no matter how much you've contributed.

- Extreme violations of a threatening, abusive, destructive or illegal nature will be addressed immediately and are not subject to 3 strikes or forgiveness.

- Contact abuse@docker.com to report abuse or appeal violations. In the case of appeals, we know that mistakes happen, and we'll work with you to come up with a fair solution if there has been a misunderstanding.

## Any question?

We’d love to hear them and help.
You can find us on [Slack](https://dockercommunity.slack.com/archives/C09DASHHRU4)
