---
paths: "**/*.{js,jsx,ts,tsx,py,go,java,php,rb,rs,c,cpp,cs}"
---

# Naming Conventions

## General Principles

- Names should reveal intent
- Avoid abbreviations (except common ones: id, url, api)
- Be consistent within the codebase
- Longer scope = longer name

## By Type

### Variables
```javascript
// Local variables: camelCase
const userName = 'alice';
const isActive = true;
const itemCount = 42;

// Constants: SCREAMING_SNAKE_CASE
const MAX_RETRIES = 3;
const API_BASE_URL = 'https://...';
```

### Functions
```javascript
// Actions: verb + noun
function createUser() {}
function validateEmail() {}
function fetchOrders() {}

// Predicates: is/has/can/should
function isValid() {}
function hasPermission() {}
function canEdit() {}

// Getters: get + noun (or just noun)
function getUserName() {}
function getActiveItems() {}
```

### Classes/Types
```javascript
// PascalCase, nouns
class UserService {}
class OrderRepository {}
interface PaymentProvider {}
type UserRole = 'admin' | 'user';
```

### Files
```
# Components: PascalCase
UserProfile.tsx
OrderList.jsx

# Utilities: camelCase or kebab-case
formatDate.ts
string-utils.ts

# Tests: match source file
UserService.test.ts
formatDate.spec.ts
```

### Directories
```
# kebab-case
src/
├── user-management/
├── order-processing/
└── api-clients/
```

## Patterns

### Boolean Variables
```javascript
// Prefix with is/has/can/should
const isLoading = true;
const hasError = false;
const canSubmit = true;
const shouldRefresh = false;
```

### Collections
```javascript
// Plural nouns
const users = [];
const orderItems = [];
const activeConnections = new Map();
```

### Handlers/Callbacks
```javascript
// on + Event
function onSubmit() {}
function onUserCreated() {}
function handleClick() {}
```

## Avoid

- Single letter names (except loops: i, j, k)
- Hungarian notation (strName, boolIsActive)
- Meaningless names (data, info, item, thing)
- Negatives in booleans (isNotValid → isInvalid)
