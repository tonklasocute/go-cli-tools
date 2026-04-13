<!--
  ⚡ go-cli-tools
  ZOLDYCK · TRANSMUTER · GODSPEED MODE
-->

<div align="center">

<img src="https://capsule-render.vercel.app/api?type=rect&height=36&color=0:020209,40:050d2e,60:050d2e,100:020209&text=%E2%9A%A1%20GO+CLI+TOOLS%20%E2%9A%A1&fontSize=14&fontColor=00d4ff&fontAlignY=65" width="100%"/>

<br/>

<img src="https://readme-typing-svg.demolab.com?font=Orbitron&weight=900&size=46&duration=4000&pause=4000&color=00D4FF&center=true&vCenter=true&width=760&height=80&lines=GO+CLI+TOOLS" alt="title"/>

<img src="https://readme-typing-svg.demolab.com?font=Share+Tech+Mono&size=15&duration=3000&pause=1000&color=4a9aba&center=true&vCenter=true&width=700&height=32&lines=%E2%9A%A1+A+collection+of+useful+CLI+tools+built+with+Go;%F0%9F%90%B9+Fast+%E2%80%A2+Minimal+%E2%80%A2+Single+binary+%E2%80%A2+Cross-platform" alt="subtitle"/>

<br/>

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="75%"/>

<br/>

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white&labelColor=00687a)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-00d4ff?style=for-the-badge&labelColor=050d2e)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-4a9aba?style=for-the-badge&labelColor=050d2e)](https://golang.org)

</div>

<br/>

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="100%"/>

## ⚡ &nbsp;Tools

| Tool | Description | Usage |
|------|-------------|-------|
| `envcheck` | Validate `.env` files against a schema | `envcheck -f .env -s .env.example` |
| `portscout` | Scan open ports on a host | `portscout -h localhost -p 1-9999` |
| `jsonformat` | Pretty-print and validate JSON files | `jsonformat -f data.json` |
| `dirsize` | Show directory size breakdown | `dirsize -p ./src --depth 2` |
| `reqping` | HTTP health check with status report | `reqping -u https://api.example.com` |

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="100%"/>

## 🚀 &nbsp;Installation

### Install All Tools

```bash
git clone https://github.com/KhomkritTK/go-cli-tools.git
cd go-cli-tools
make install-all
```

### Install a Specific Tool

```bash
go install github.com/KhomkritTK/go-cli-tools/cmd/envcheck@latest
go install github.com/KhomkritTK/go-cli-tools/cmd/portscout@latest
go install github.com/KhomkritTK/go-cli-tools/cmd/jsonformat@latest
```

### Build Binaries Locally

```bash
make build-all

# Binaries will be in ./bin/
./bin/envcheck --help
```

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="100%"/>

## 🗂️ &nbsp;Project Structure

```
go-cli-tools/
├── cmd/
│   ├── envcheck/       # .env validator
│   ├── portscout/      # Port scanner
│   ├── jsonformat/     # JSON formatter
│   ├── dirsize/        # Directory size analyzer
│   └── reqping/        # HTTP health checker
├── internal/
│   └── shared/         # Shared utilities (output formatting, errors)
├── Makefile
└── README.md
```

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="100%"/>

## 🛠️ &nbsp;Makefile Commands

```bash
make build-all      # Build all tool binaries
make install-all    # Install all tools to $GOPATH/bin
make test           # Run all tests
make lint           # Run golangci-lint
make clean          # Remove build artifacts
```

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="100%"/>

## 🧱 &nbsp;Tech Stack

<div align="center">

[![](https://skillicons.dev/icons?i=go,git,github,linux&theme=dark)](https://skillicons.dev)

</div>

<img src="https://capsule-render.vercel.app/api?type=rect&height=2&color=0:020209,20:003d5c,50:00d4ff,80:003d5c,100:020209" width="100%"/>

## 🤝 &nbsp;Contributing

Have a useful CLI tool idea? PRs are welcome!

```bash
git checkout -b feat/new-tool
# Add your tool under cmd/<tool-name>/
git commit -m "feat: add <tool-name>"
git push origin feat/new-tool
```

## 📄 &nbsp;License

Released under the [MIT License](LICENSE).

<div align="center">

<img src="https://capsule-render.vercel.app/api?type=waving&height=120&color=0:020209,30:030e1c,60:041428,100:020209&section=footer" width="100%"/>

</div>
