# Code Review Checklist

## Correctness
- [ ] Logic is correct and handles edge cases
- [ ] Error handling is appropriate
- [ ] No obvious bugs or off-by-one errors

## Code Quality
- [ ] Code is readable and well-named
- [ ] No unnecessary duplication
- [ ] Functions are appropriately sized

## Security
- [ ] No injection vulnerabilities
- [ ] Input is validated at boundaries
- [ ] No sensitive data exposed in logs or responses

## Performance
- [ ] No unnecessary allocations in hot paths
- [ ] Database queries are efficient
- [ ] No N+1 query problems

## Tests
- [ ] New code has test coverage
- [ ] Edge cases are tested
- [ ] Tests are meaningful, not just for coverage
