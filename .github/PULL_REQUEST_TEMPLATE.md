## Summary

<!-- What does this PR do? Closes #N if applicable. -->

## Checklist

- [ ] No business logic in HTTP handlers — service layer only
- [ ] New secrets use `EncryptedString`, no plaintext storage
- [ ] No raw SQL — GORM or `applyConstraints()` only
- [ ] Auth enforced on all new/modified routes
- [ ] `.env.example` updated if new env vars added
