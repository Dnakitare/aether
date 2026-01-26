---
name: scaffold
description: Bootstrap new project structure for any tech stack. Use when starting a new project, creating boilerplate, or setting up project structure.
hooks:
  stop:
    - command: ".claude/hooks/validators/code-quality.sh"
      timeout: 60
---

# Scaffold

Rapidly create project structure and boilerplate for any tech stack.

## Usage

```
/scaffold [project-type] [options]
```

## Examples

```
/scaffold react-app with TypeScript and Tailwind
/scaffold python-api using FastAPI
/scaffold go-cli for file processing
/scaffold node-backend with Express and PostgreSQL
```

## Process

1. **Identify Requirements**
   - What type of project?
   - Which technologies?
   - Any specific features needed?

2. **Create Structure**
   - Standard directory layout
   - Configuration files
   - Package/dependency files

3. **Add Boilerplate**
   - Entry point files
   - Basic configuration
   - Development tooling

4. **Setup Tooling**
   - Linting/formatting
   - Testing framework
   - Git hooks (if needed)

## Common Project Types

### React/Next.js
```
src/
├── components/
├── pages/ or app/
├── hooks/
├── lib/
├── styles/
└── types/
```

### Python API
```
src/
├── api/
├── models/
├── services/
├── tests/
└── config/
```

### Go Service
```
cmd/
├── app/
internal/
├── handlers/
├── services/
├── models/
pkg/
```

### Node.js Backend
```
src/
├── routes/
├── controllers/
├── models/
├── middleware/
├── services/
└── utils/
```

## Standard Files to Create

- `README.md` - Project documentation
- `.gitignore` - Git ignore rules
- `package.json` / `go.mod` / `requirements.txt` - Dependencies
- Config files (tsconfig, eslint, pytest, etc.)
- `.env.example` - Environment template
- `Dockerfile` (optional)

## Output

After scaffolding:
1. List created files
2. Show next steps
3. Provide quick start commands
