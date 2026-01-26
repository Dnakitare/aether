# Create API Endpoint

Create a new API endpoint with proper error handling, validation, and types.

## Arguments
- $RESOURCE - The resource name (e.g., users, orders)
- $METHOD - HTTP method (GET, POST, PUT, DELETE)

## Instructions

Create a new $METHOD endpoint for $RESOURCE with:

1. **Input validation** using zod or similar
2. **Type-safe request/response** interfaces
3. **Proper error handling** with meaningful error codes
4. **Authentication middleware** if needed
5. **Rate limiting** consideration

Follow the existing patterns in the codebase.

## Example Usage
```
/api-endpoint users POST
/api-endpoint orders GET
```
