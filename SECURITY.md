# Security policy

## Reporting a vulnerability

Use the repository's [Security tab](https://github.com/tyler180/dynasty-ff-backend/security)
to submit a private vulnerability report if that option is available. Otherwise,
contact the repository maintainer privately. Include the affected revision or
deployment, the impact, and enough sanitized detail to investigate. Keep live
credentials, private league data, and vulnerability details out of public issues
and pull requests.

## Security review guidelines

- **Public API boundary.** Keep application routes behind API Gateway JWT
  authorization with the configured OIDC issuer and audience. The public
  `GET /health` endpoint must return no league or dependency details. Treat
  CORS as a browser policy. Verify that HTTP routing cannot dispatch arbitrary
  direct Lambda actions, including identity writes and arbitrary snapshot writes.
- **League access.** Validate caller-supplied season, league, franchise, and
  player identifiers before using them in provider calls or storage keys.
  If a change introduces data for leagues with different access permissions,
  verify that authenticated callers can only read or sync the leagues they
  are authorized to access.
- **Credentials and logs.** Load Lambda provider credentials from the
  configured Secrets Manager secret. Prefer the read-only MFL owner API key.
  Keep tokens, MFL cookies, API keys, authorization headers, and secret
  payloads out of source, command arguments, Terraform values, logs, and
  responses. Review error paths as well as successful requests.
- **AWS access and CI.** Scope IAM actions and resources to the intended
  Lambda, tables, bucket prefixes, secrets, and deployment roles. Review
  API Gateway and EventBridge invoke permissions. Check GitHub Actions
  workflow permissions and OIDC trust when deployment automation changes.
  Keep untrusted pull request code away from privileged credentials.
- **External data and resource use.** Treat provider responses, imported files,
  and HTTP request bodies as untrusted. Review changes for bounded requests,
  timeouts, and cancellation. Flag user input that can redirect outbound
  requests, cross a storage namespace, or cause uncontrolled provider or
  AWS work.
- **Data exposure.** Review changes to bucket policies, log fields, fixtures,
  and response payloads for exposure of private league data or credentials.
  Return sanitized API errors and preserve provider data-use restrictions.
