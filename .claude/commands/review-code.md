# Review Recent Code

Start a fresh review of recently written code before shipping.

## Arguments
- $FILES - Optional: specific files to review (defaults to recently modified)

## Instructions

Review the code with fresh eyes, checking for:

1. **Security Issues**
   - No hardcoded secrets
   - Input validation present
   - Auth checks in place

2. **Performance**
   - No N+1 queries
   - Appropriate caching
   - No memory leaks

3. **Error Handling**
   - Errors caught and handled
   - Meaningful error messages
   - No silent failures

4. **Code Quality**
   - Clear naming
   - No unnecessary complexity
   - Proper separation of concerns

5. **Edge Cases**
   - Null/undefined handled
   - Empty arrays/objects considered
   - Boundary conditions tested

Report findings with severity levels.
