# Changelog

## [0.9.0](https://github.com/meshploy/meshploy/compare/v0.8.0...v0.9.0) (2026-09-04)


### Features

* add config file tools to the MCP surface ([cb06a7c](https://github.com/meshploy/meshploy/commit/cb06a7c6f6f486816214dc37e826894d7402876f))
* add config files that project a file into a service at a path ([8fa5106](https://github.com/meshploy/meshploy/commit/8fa5106441dca90ea79e127325697ac902c0f1a3))
* create config files on the new-resource page instead of a dialog ([38b99bb](https://github.com/meshploy/meshploy/commit/38b99bba44e265a13e69ed17078a8a88dbb083a4))
* encrypt kubernetes secrets at rest on new gateway installs ([3396e24](https://github.com/meshploy/meshploy/commit/3396e24e970f2eb736eb9f417913119eb6deddc8))
* fix a service's kubernetes name at creation instead of deriving it from its name ([09b148b](https://github.com/meshploy/meshploy/commit/09b148bf4c727489d0522b352a616e130e6b36e5))
* give a config file a detail page listing the services that mount it ([39530c9](https://github.com/meshploy/meshploy/commit/39530c9a8117ed0969162666152ca633beb253d3))
* let a template write a credential's bcrypt hash into a config file ([db7d10b](https://github.com/meshploy/meshploy/commit/db7d10bb1b69a8162472f4b365ea347c998b0e2d))
* let an orphaned workload be removed from the cluster page ([166fee5](https://github.com/meshploy/meshploy/commit/166fee5dc2a09565aa3cf5ace2570e4b8ccd33a3))
* let the template catalog be refreshed on demand instead of only on a timer ([d3d0b82](https://github.com/meshploy/meshploy/commit/d3d0b82189b38e2c5006b12e3953ec80938ccbce))
* list a stack's config files beside its volumes and routes ([ddd4f86](https://github.com/meshploy/meshploy/commit/ddd4f86ed2d703a30bce90081f2286f67449ea8d))
* mark edge builds with their commit and channel instead of a bare release version ([6691d3c](https://github.com/meshploy/meshploy/commit/6691d3c15b81118d510b1a0cb3153967fb87aa53))
* prefill the config file parent path and share one editor between both forms ([0c109eb](https://github.com/meshploy/meshploy/commit/0c109ebd2e8e5ee898bd26fde5ff6ce8d5894cf4))
* report cluster workloads that no service owns ([62ed54a](https://github.com/meshploy/meshploy/commit/62ed54aff5c454950a3592fa0914761bc91f1ecb))
* serve a single config file scoped to its project ([0b4ea9c](https://github.com/meshploy/meshploy/commit/0b4ea9cb15660aac6616ff662c906f1d4c467a40))
* show the config file count on the project tab bar ([1417848](https://github.com/meshploy/meshploy/commit/14178487979a5af8b664b60e6cc5a03c4a486cb0))
* tell an edge install when main has moved, named as an edge update ([633e648](https://github.com/meshploy/meshploy/commit/633e648c66bf84edbbbfb4a538e35e00881630d8))


### Bug Fixes

* keep compose interpolation out of a config file's contents ([f8aeb08](https://github.com/meshploy/meshploy/commit/f8aeb08eb7d7441f35a2c13e59f87535c7f91334))
* number a repeated template's stack so it cannot adopt the first one's volume ([6fb94cf](https://github.com/meshploy/meshploy/commit/6fb94cf8ece5bdc053c5ac5f5f834fa7672b5d86))
* read a missing node port from the cluster instead of refusing the route ([bb3b1f5](https://github.com/meshploy/meshploy/commit/bb3b1f59ec2c5161d5c5340b47a93555e39022e5))
* replace the old workload when a service is renamed ([c3d9491](https://github.com/meshploy/meshploy/commit/c3d9491dba2c121b7e6751183eae76dc29681015))
* stop huma dropping a config file's fields from the detail response ([2706a53](https://github.com/meshploy/meshploy/commit/2706a53497376d9332bbbf72c905713fd9f7fa68))
* stop the status reconciler locking itself out of its own applying write ([dbc12d1](https://github.com/meshploy/meshploy/commit/dbc12d117055f54661219cbddf97f3f2416755f9))
* stream a deployment's log live when there is no build pod to follow ([284de53](https://github.com/meshploy/meshploy/commit/284de5389920285d903cabf67315c0c367709962))

## [0.8.0](https://github.com/meshploy/meshploy/compare/v0.7.0...v0.8.0) (2026-09-02)


### Features

* add a per-client MCP configuration picker to the agent connect panel ([8672b14](https://github.com/meshploy/meshploy/commit/8672b14d880ae2780f48108858e6da3d65217152))
* add licence status and activate commands with an MCP tool extension hook ([ea71d6d](https://github.com/meshploy/meshploy/commit/ea71d6d6d3f7484b7cd2813f07fab41f1911f208))
* add stack destroy to tear down its services while keeping the stack ([3ed37ba](https://github.com/meshploy/meshploy/commit/3ed37ba87392f230052cd9bb45d142df6ce7e197))
* derive stack status from its services instead of leaving it idle ([028d8ae](https://github.com/meshploy/meshploy/commit/028d8ae426debd3510efa86f6ecdf64d10ab826e))
* follow the rollout and stream cluster events into the deployment log ([3ea5431](https://github.com/meshploy/meshploy/commit/3ea543138ad138f13ceb55dccc3e16bf3a36fe00))
* keep every status pill polling so a settled view cannot go stale ([72b46dd](https://github.com/meshploy/meshploy/commit/72b46ddb2717c9d40db761a7ef040fdf89bb2c8d))
* let stack destroy optionally remove its volumes and routes ([3b1f924](https://github.com/meshploy/meshploy/commit/3b1f924da3ef72602ea63c147d420fa966a1fc1c))
* mark stack-owned services, volumes and routes with a stack pill ([e3c4c66](https://github.com/meshploy/meshploy/commit/e3c4c661371587a0f92ae7614ced2a8700559c78))
* show a stack's routes and volumes alongside its services ([6cf3337](https://github.com/meshploy/meshploy/commit/6cf3337b5739324384436d4d14eeebec1cb0c8d5))
* show environment and volumes in the visual stack editor ([a4a2c3e](https://github.com/meshploy/meshploy/commit/a4a2c3e2bdfe4a9736fe1c99ff00d6d695145505))


### Bug Fixes

* address a database by its real workload name so status stop and delete work ([20fdf77](https://github.com/meshploy/meshploy/commit/20fdf77ca6c44d099237485129bde33c30f8904a))
* bind flannel to the mesh interface so pod traffic fits the wireguard mtu ([bda5edf](https://github.com/meshploy/meshploy/commit/bda5edf37904288413c2a597d2d95909cd73f5f3))
* cascade service and group deletes so attachments do not block them ([575b877](https://github.com/meshploy/meshploy/commit/575b877ab2b1915eb4c0ecbb6d4316b7439a3801))
* create template routes once the service has a routable port ([44f87c6](https://github.com/meshploy/meshploy/commit/44f87c66898c8e7ef1397144ceea58f27277d7b7))
* deploy on apply, re-apply on start, clean up k8s on delete, derive status from the cluster ([49596e0](https://github.com/meshploy/meshploy/commit/49596e0c8fb1a5cb15d724b680d85c8e68987d0c))
* derive volume status from the claim and rename pending to idle ([8533b79](https://github.com/meshploy/meshploy/commit/8533b797da1fba05739613c259fef8177f5bff6e))
* give every workload the same resource defaults so none deploys unbounded ([ffbd4f5](https://github.com/meshploy/meshploy/commit/ffbd4f50f6776fb04072ba0247fa55f2000b6b0a))
* give pods a resolver without the mesh search domains on every node ([33b2ddd](https://github.com/meshploy/meshploy/commit/33b2ddd9faa93cbc89a94a08f494d9ab6890b582))
* give the Headscale API key a long expiry so mesh access does not silently lapse ([63ba221](https://github.com/meshploy/meshploy/commit/63ba22130e3f5ede581191c3d8fe3680b4f7e53c))
* let a stack stuck in applying recover instead of holding that status forever ([0177b72](https://github.com/meshploy/meshploy/commit/0177b721be0cbe484ae04c4c578dcb0c5adcb312))
* let template icons load by exempting only the icon route from auth ([6aabd26](https://github.com/meshploy/meshploy/commit/6aabd26a2cd96405cd68ac0c4d1e426aef4971a8))
* let the member role dropdown open instead of navigating to the detail page ([c626d9d](https://github.com/meshploy/meshploy/commit/c626d9da283a3640f30a8ac5b9d1a5e9a6f79bc6))
* link a stack-created database to its stack ([299faf6](https://github.com/meshploy/meshploy/commit/299faf630550012124a0e422c4b602a13bd31989))
* make mcp structured content a json object so tool results validate ([86c5102](https://github.com/meshploy/meshploy/commit/86c5102c9313ce9cf2a46c4f411b61ba40c96554))
* make target node take effect and give volumes a node selector ([34e8e49](https://github.com/meshploy/meshploy/commit/34e8e49f3f9ee78955a0002a785dbd9d56758f57))
* mark a service deploying while its deployment runs ([3b823ae](https://github.com/meshploy/meshploy/commit/3b823ae4e256a527e733d56185a790be0f2bf833))
* publish a database under its compose service name so intra-stack connections resolve ([40d172b](https://github.com/meshploy/meshploy/commit/40d172b0730ea404f9e8166666e2a1ee4ec46eb4))
* refactor image upgrade to include runtime parameter and enhance authentication handling ([3b53ac5](https://github.com/meshploy/meshploy/commit/3b53ac5756705a7a0036189c469b4d8526948fd3))
* relink a stack service whose stack link was lost on the next apply ([538e069](https://github.com/meshploy/meshploy/commit/538e06936271e858c67a2f2866267eee9a2dbde3))
* return empty apply lists instead of null so the result page renders ([7edd9b6](https://github.com/meshploy/meshploy/commit/7edd9b625ae012700163c26fd04169f1d3259ca0))
* stop injecting a service's own discovery variables into itself ([0e68826](https://github.com/meshploy/meshploy/commit/0e68826c363a75cff732612b6747b2d7a667a488))
* stop the secret64 generator spinning forever on a byte overflow ([f778844](https://github.com/meshploy/meshploy/commit/f778844489d2310c4da11abbc4696cfea27bbbf8))
* stop the visual stack editor discarding spec it does not model ([ec34a8d](https://github.com/meshploy/meshploy/commit/ec34a8de4f1ef399ca8be86a88fb8ba4050c4ed9))
* surface a broken Headscale connection instead of serving stale mesh state ([937051d](https://github.com/meshploy/meshploy/commit/937051d80b6da2318d0df13e8e3eb0188b641685))
* treat a missing cluster as a skipped rollout, not a per-service failure ([7ea8382](https://github.com/meshploy/meshploy/commit/7ea8382ede1fc56cd22eab45d9053a1bfd67c4c5))

## [0.7.0](https://github.com/meshploy/meshploy/compare/v0.6.0...v0.7.0) (2026-07-30)


### Features

* add an edition comparison dialog and render EE navigation from the overlay slot ([2d7d8cc](https://github.com/meshploy/meshploy/commit/2d7d8cc3d8dcb7d2c9ca6efde3e1146c9e57bc1b))
* define licence feature flags in the shared package ([696acee](https://github.com/meshploy/meshploy/commit/696aceeafaac948e68620c803b2a1babb28a6367))
* use the mesh mark for icons and stop syncing the docs logo from the favicon ([652f208](https://github.com/meshploy/meshploy/commit/652f2087f2b15443675038ce3f1e5a6bedc3198b))


### Bug Fixes

* guard the provisioning token button until the org resolves ([bb35b06](https://github.com/meshploy/meshploy/commit/bb35b06a419c7fadecac89d1127e3bdd4ef84911))

## [0.6.0](https://github.com/meshploy/meshploy/compare/v0.5.0...v0.6.0) (2026-07-29)


### Features

* add extension hooks for routes, middleware, quotas, and job mutation ([0283c49](https://github.com/meshploy/meshploy/commit/0283c49c704520ab08223840d78bb25c98dc283d))
* add go:generate command to sync embedded template snapshot ([a0a2abc](https://github.com/meshploy/meshploy/commit/a0a2abce605b9c7c0f6a7056a21cf0b7e62ca9fa))
* add license verification and entitlement service ([df468f1](https://github.com/meshploy/meshploy/commit/df468f1d38d245436d85dc680e15637fa52fb231))
* add MSW demo mode and Playwright E2E tests ([8d5d463](https://github.com/meshploy/meshploy/commit/8d5d46391ba94cbfcaeb4f71df40b556b1c46744))
* add Starlight docs site ([ded88d0](https://github.com/meshploy/meshploy/commit/ded88d0913fc829c474518d37432c99dfca9f298))
* add template gallery with logos, deploy flow, and offline fallback ([b1c5afb](https://github.com/meshploy/meshploy/commit/b1c5afb8bed6b1851c295b0850cadd410d9cb99e))
* add template management functionality ([3aab39f](https://github.com/meshploy/meshploy/commit/3aab39fe327fc116e61aa94de01bff7a0014db92))
* agent-first platform — token-authed agent principals, gateway-served remote MCP, and one-shot declarative apply ([10670c4](https://github.com/meshploy/meshploy/commit/10670c4fa41dcd06d98f1fa6795f7535c386389c))
* fetch template catalog from meshploy-templates repo ([8ec5451](https://github.com/meshploy/meshploy/commit/8ec54512c503a07f7bc94020ba08e1c081f97a22))
* implement on-demand TLS support and update installation script for DNS mode selection ([4589661](https://github.com/meshploy/meshploy/commit/4589661cad14546c6909879ae278a803d2f39423))
* make the API image overridable and add licence activation to settings ([b6a384d](https://github.com/meshploy/meshploy/commit/b6a384d871f072f01ee42b7255127bd15ab7c71c))
* switch to the Enterprise image from the CLI and explain the path in the UI ([51d6f81](https://github.com/meshploy/meshploy/commit/51d6f814651351f7303db2a0674826bdb9d6d454))


### Bug Fixes

* add templates mock handlers and layout update ([c9847ee](https://github.com/meshploy/meshploy/commit/c9847eebd79dd8f8406b23d2cd08addcdbdfdc00))
* anchor the auth allowlist and remove the blanket GET /api/ exemption ([5aeb610](https://github.com/meshploy/meshploy/commit/5aeb6103dbc88876c980071bb71f28ab89b96c7f))
* copy license and server module manifests in the proxy image build ([dddadcd](https://github.com/meshploy/meshploy/commit/dddadcd3026fc72dc2a90b4589b7517f68665c28))
* fix template gallery type errors and missing GitHub icon export ([f9fa938](https://github.com/meshploy/meshploy/commit/f9fa938ef72b19ec7a206e8131c560425d19f7e9))
* repair CI checks after the packages/server move ([44f766a](https://github.com/meshploy/meshploy/commit/44f766ae3558513d7d8373d95e4ec49f274325de))
* rewrite docs anchor links and trim changelog ([446919f](https://github.com/meshploy/meshploy/commit/446919f8c6bd07f78fcceb21a09c6ac4aa8b24aa))
* scope cluster credential endpoints to an org and require admin ([7bbff3d](https://github.com/meshploy/meshploy/commit/7bbff3de5cedf6ef84eb4a2e4de3f11d387fa0a6))
* scope the pod terminal to its org and replace the JWT query param with single-use tickets ([5a7f594](https://github.com/meshploy/meshploy/commit/5a7f594ef0498e299d406f0d8f2142b59438a08e))
* verify the cluster certificate when K3S_SERVER_URL rewrites the address ([7409f3d](https://github.com/meshploy/meshploy/commit/7409f3d3ab7f9f828ebac1869e790c16b2f28211))

## [0.5.0](https://github.com/meshploy/meshploy/compare/v0.4.0...v0.5.0) (2026-06-22)


### Features

* expand MCP server tools and CLI client coverage ([b479bb3](https://github.com/meshploy/meshploy/commit/b479bb3eebdffc058e6acad1a30150b656eb57de))
* add job.failed and node.offline notification dispatch ([1a1b0c5](https://github.com/meshploy/meshploy/commit/1a1b0c57afafa197f55700553224b2f4adc68801))
* fine-grained resource permissions with action-based access control ([ae6d218](https://github.com/meshploy/meshploy/commit/ae6d21885aebface8b88f2fe4480ae63a706baaf))
* invite token sign-up flow with role assignment and member role change UI ([e7cc390](https://github.com/meshploy/meshploy/commit/e7cc390e4ac83cd920f627b308547c3368638808))
* users page with member permissions UI and role-based frontend guards ([651777b](https://github.com/meshploy/meshploy/commit/651777b35e201f530b60a956bd0652caea35ba8c))
* resource-centric permissions UI with service/stack/job permissions tabs and gap fixes for routes, volumes and variable groups ([f6634a3](https://github.com/meshploy/meshploy/commit/f6634a362d10a2582dc91dba520a443bb4d4b719))
* add permissions routes for stacks and services in route tree ([b4606b2](https://github.com/meshploy/meshploy/commit/b4606b2ff4494e37bd60439422c88f253625211c))


### Bug Fixes

* fix terminology for clarity ([8deeabb](https://github.com/meshploy/meshploy/commit/8deeabb9a744a883c8f0b17567f12a282665682c))
* harden invitation error messages and restrict headscale preauth key to admins ([354cf92](https://github.com/meshploy/meshploy/commit/354cf921dc2699c5f57483807557b2341d1c909a))
* close auth check gaps in multiple handlers ([a139626](https://github.com/meshploy/meshploy/commit/a1396261fb0378d8f33960ce70ac354c19f11fc4))
* enforce resource-level permissions across all handler endpoints ([1ab2252](https://github.com/meshploy/meshploy/commit/1ab2252afa4bace6269f7b2a13461d11aeb9f4ee))
* enforce resource-level permissions on deployment handlers and close auth gaps and git integration handlers ([2525e7f](https://github.com/meshploy/meshploy/commit/2525e7fa609cb5257c5c728d9dba98c804652cb4))
* resource-centric permissions UI with project/resource grant distinction ([0adb0cf](https://github.com/meshploy/meshploy/commit/0adb0cfc7416a632c61b8ef58e0037f1bc95b246))


### Refactoring

* consolidate member management into /users with permissions UI and role-based nav guards ([f360c0f](https://github.com/meshploy/meshploy/commit/f360c0fb71bc516b4cc00cf8afd3a93e851e2856))


### Tests

* add comprehensive CLI test suite covering client, config, and cmd helpers ([48a9f76](https://github.com/meshploy/meshploy/commit/48a9f76bf3a7d23589dd12587990acf8b8b89ddd))


### Documentation

* rewrite concepts, how-it-works, and other docs, harden install preflight checks ([13c0e61](https://github.com/meshploy/meshploy/commit/13c0e61d84b2819504a579d617abad42ebe0b73b))
* add contributing and security policy documentation ([1a9ec4c](https://github.com/meshploy/meshploy/commit/1a9ec4c033b6477cfd0fa410f0f5c485f08043f3))
* sync all documentation with current codebase state ([7d75f0b](https://github.com/meshploy/meshploy/commit/7d75f0b861a1824141d8bdf264c666564f856235))

## [0.4.0](https://github.com/meshploy/meshploy/compare/v0.3.2...v0.4.0) (2026-06-05)


### Features

* unify channel selection across get.sh, install.sh, and server-upgrade ([912f1c4](https://github.com/meshploy/meshploy/commit/912f1c4b4fa7e03560e6f6d636647b4197683be4))


### Bug Fixes

* eliminate open DNS resolver and fix Headscale DNS with split nameservers ([2d9fcaf](https://github.com/meshploy/meshploy/commit/2d9fcaf60e0aa340c977bbe4ab5244cd39de08cb))

## [0.3.2](https://github.com/meshploy/meshploy/compare/v0.3.1...v0.3.2) (2026-06-04)


### Bug Fixes

* update deployment process to sync configs and substitute Corefile values ([f07c576](https://github.com/meshploy/meshploy/commit/f07c5760e4f2016671a40c533a780c2a462638f2))

## [0.3.1](https://github.com/meshploy/meshploy/compare/v0.3.0...v0.3.1) (2026-06-04)


### Bug Fixes

* update CoreDNS configuration and add DNS settings for services in docker-compose ([7ba38ad](https://github.com/meshploy/meshploy/commit/7ba38ad705fd9cb879864dddf06738d44b58cb09))

## [0.3.0](https://github.com/meshploy/meshploy/compare/v0.2.8...v0.3.0) (2026-06-04)


### Features

* server-upgrade cmd to sync deploy configs and pull latest images and, fix: adjust CoreDNS configuration to enhance security ([90c79a7](https://github.com/meshploy/meshploy/commit/90c79a7e9cefae0c2217c5a82be071771fdf9134))

## [0.2.8](https://github.com/meshploy/meshploy/compare/v0.2.7...v0.2.8) (2026-06-01)


### Bug Fixes

* rebuild all images and stamp version on tag push ([a382826](https://github.com/meshploy/meshploy/commit/a3828264ac571f5cc691040481f4cb591ad82dd3))

## [0.2.7](https://github.com/meshploy/meshploy/compare/v0.2.6...v0.2.7) (2026-05-27)


### Bug Fixes

* update CLI release process and improve envLanguage state management ([c7dfbe5](https://github.com/meshploy/meshploy/commit/c7dfbe54973603bc07887f2de31f1493b57e6c40))

## [0.2.6](https://github.com/meshploy/meshploy/compare/v0.2.5...v0.2.6) (2026-05-27)


### Bug Fixes

* stable CLI update by default, edge flag for rolling builds, sync VERSION ([b0ecac0](https://github.com/meshploy/meshploy/commit/b0ecac077d48cfbb09fa30c5abb67f636ac568fb))

## [0.2.5](https://github.com/meshploy/meshploy/compare/v0.2.4...v0.2.5) (2026-05-27)


### Bug Fixes

* detect release-please merge commit to push latest image tag ([009296c](https://github.com/meshploy/meshploy/commit/009296cbcde4cb979c310ae9433fa744fe6f129b))

## [0.2.4](https://github.com/meshploy/meshploy/compare/v0.2.3...v0.2.4) (2026-05-27)


### Bug Fixes

* syntax-highlighted env editor and unescape \n in env var values ([568b676](https://github.com/meshploy/meshploy/commit/568b676b4ba6c3506001d2becf1d185f64d56c45))

## [0.2.3](https://github.com/meshploy/meshploy/compare/v0.2.2...v0.2.3) (2026-05-27)


### Bug Fixes

* resolve circular type reference in refetchInterval callbacks ([5322795](https://github.com/meshploy/meshploy/commit/5322795c971db40f3cfc4c8b6fb17c3b3e84e165))

## [0.2.2](https://github.com/meshploy/meshploy/compare/v0.2.1...v0.2.2) (2026-05-27)


### Bug Fixes

* push latest image tag on release-please merge commit ([a9ad225](https://github.com/meshploy/meshploy/commit/a9ad22590f772f810841c6f6f0b25070f503c59e))

## [0.2.1](https://github.com/meshploy/meshploy/compare/v0.2.0...v0.2.1) (2026-05-26)


### Bug Fixes

* auto-refresh polling missing for databases and stacks list tabs ([59884a6](https://github.com/meshploy/meshploy/commit/59884a683761ebb6211fe71953abb075f46dfd77))
* base64-encode auth field in imagePullSecret dockerconfigjson ([951f568](https://github.com/meshploy/meshploy/commit/951f568800267fcfbf2f943bc27fd39ea9be1b53))
* close registration after first owner account is created ([5793eab](https://github.com/meshploy/meshploy/commit/5793eaba8288981ca7702f7a656c78315f5833ad))

## [0.2.0](https://github.com/meshploy/meshploy/compare/v0.1.2...v0.2.0) (2026-05-26)


### Features

* add 2FA recovery codes and guard SetupTOTP against re-enrollment ([7fcd754](https://github.com/meshploy/meshploy/commit/7fcd7540a40bd94f94ca5d71c51709d1650f4303))
* auto-deploy on git push using GitHub App webhook and per-service deploy token ([f6b476c](https://github.com/meshploy/meshploy/commit/f6b476c7835be35dfd783299954c268d9270e9e2))
* **cli:** version cmd, deployment subcommands, full command reference in README ([7eed267](https://github.com/meshploy/meshploy/commit/7eed2674b60469177370960059286a4bf070cc85))


### Bug Fixes

* **deploy:** rename subdomain app to console, expand reserved subdomain list ([ef1fdb5](https://github.com/meshploy/meshploy/commit/ef1fdb5a45dd14046b809f07a903a40031b2aede))
* exempt TOTP, recovery, and webhook routes from Bearer JWT check ([4b15bf0](https://github.com/meshploy/meshploy/commit/4b15bf09b1983a553cfab859a3cf4583638d0cf4))
* update build.context subdirectory through stack sync and build job ROOT_DIR ([6c3c99d](https://github.com/meshploy/meshploy/commit/6c3c99d878282954228003a28f88229dc603471e))

## [0.1.2](https://github.com/meshploy/meshploy/compare/v0.1.1...v0.1.2) (2026-05-25)


### Features

* add git source for stacks - file/repo fetch modes, auto-detect build context, sync endpoint, read-only editor, managed-by-stack banner in service config ([e5c74d5](https://github.com/meshploy/meshploy/commit/e5c74d5e2535a7f516552cd732f3043e89aaa7db))


### Bug Fixes

* pass correct VERSION build arg only on tag pushes ([3d76b9b](https://github.com/meshploy/meshploy/commit/3d76b9b472d107712787371fd6b167d08f5b2ab4))
* separate rolling/stable image channels and update fetch-mode segmented control width ([1bf6244](https://github.com/meshploy/meshploy/commit/1bf6244d8696f70c7747685279316ea975d00e4a))

## [0.1.1](https://github.com/meshploy/meshploy/compare/v0.1.0...v0.1.1) (2026-05-25)


### Features

* add .npmrc for platform-specific binaries support in Docker ([7ba78e0](https://github.com/meshploy/meshploy/commit/7ba78e05533a4501ad8d612b13be285c6f731975))
* add account page, redirect target in new route form, and user menu improvements ([32e5c4d](https://github.com/meshploy/meshploy/commit/32e5c4de549f0e3bfa8969a0c5ae553d988453f6))
* add appearance section for accent color customization in settings page ([defb607](https://github.com/meshploy/meshploy/commit/defb607148e3fb51ef8de9f8993d7630d3046295))
* add backup management functionality ([8c6deaf](https://github.com/meshploy/meshploy/commit/8c6deaf452b5f1501acc8825e48a94cb30ae6d28))
* add backup restore functionality and UI components for managing restore points ([0fe22db](https://github.com/meshploy/meshploy/commit/0fe22db5749d70be7901dc9d9e16216c32c0828e))
* add backup triggering functionality and UI components for backup management ([c76d4dd](https://github.com/meshploy/meshploy/commit/c76d4dda1a26a91e4cc41faf159fdf54ac34dae7))
* add build cache management with clear cache endpoint and UI integration ([699f9e9](https://github.com/meshploy/meshploy/commit/699f9e91606aa06480caa2e001814560215d040c))
* add build-time environment variables support for services and update related components ([db1f9ee](https://github.com/meshploy/meshploy/commit/db1f9ee9f2c780bd45bc169f2bf8ad71df480a32))
* add builder node and resource request support for build configurations ([d76e9d7](https://github.com/meshploy/meshploy/commit/d76e9d75d3de0cf5d89866438bc850bf3dabd3f2))
* add built-in container registry support and related configurations ([12edec0](https://github.com/meshploy/meshploy/commit/12edec0f825b79d171e5ac274874e47deb3c0005))
* add cancel and delete deployment functionality with API integration ([11cde56](https://github.com/meshploy/meshploy/commit/11cde5628620b627ee993dcc590e119a7a606de9))
* add CLI for managing nodes and authentication, including install/uninstall commands ([eadfaa9](https://github.com/meshploy/meshploy/commit/eadfaa928d0c652ee36dc699d0df990b9a046209))
* add CLI tool documentation and update uninstall script for worker detection ([79e9c18](https://github.com/meshploy/meshploy/commit/79e9c184eb7976509aa9f6b71754bd840225fe4b))
* add CodeMirror for script editing in job form ([a4d7466](https://github.com/meshploy/meshploy/commit/a4d7466dcc95ae74fce6f136eb96df78a5ec04e0))
* add commands for managing jobs, stacks, and volumes in CLI ([e1108d3](https://github.com/meshploy/meshploy/commit/e1108d35aeba7804c1279aa1cd0b082944e93662))
* add cron schedule support for manual run jobs and delete run history support; update styles ([07e4110](https://github.com/meshploy/meshploy/commit/07e411058273d2bd8643464d11c7b211812023f6))
* add database support in service creation and enhance database management UI ([e71b569](https://github.com/meshploy/meshploy/commit/e71b5690b15f45a8bc7b08044b2ed3048abeadb0))
* add deploy job to workflow and update Dockerfile to set ownership for config directory ([ad72b97](https://github.com/meshploy/meshploy/commit/ad72b9763a4a1dee55b152d1ba31f103be274d50))
* add deployment log streaming and management features ([32261cb](https://github.com/meshploy/meshploy/commit/32261cbd4f3f39a4a94582f9ca43ddd76af0aa63))
* add email configuration management with CRUD operations and UI integration ([9cc9df5](https://github.com/meshploy/meshploy/commit/9cc9df52375e08ec5363fb551d59e3c4a517ef43))
* add endpoint to set build-time environment variables for services and update related API calls ([8905457](https://github.com/meshploy/meshploy/commit/8905457991199661fa8b4e44d6b9cfb1bed23eac))
* add external link icon to route details and enhance layout for better visibility ([47377ac](https://github.com/meshploy/meshploy/commit/47377acb388234eb3f9d1dd18d82c54626115e45))
* add GitLab and Gitea git source integrations with PAT and OAuth App auth methods ([4f4ab29](https://github.com/meshploy/meshploy/commit/4f4ab2919e3c6ef6302bd19640f26c96e33be38c))
* add Headscale FQDN to Node response and related UI components ([407c744](https://github.com/meshploy/meshploy/commit/407c7446b52630ceb1f317c99f81f36447138a9f))
* add Headscale preauth key generation and related UI components ([00154bc](https://github.com/meshploy/meshploy/commit/00154bce40a11fba9e7cef01bbe9e84c5a04d40c))
* add Headscale preauth key management and update related UI components ([417187f](https://github.com/meshploy/meshploy/commit/417187f6826ce0dc824adee281471967b07a461f))
* add in-app route detail navigation from service overview ([e3b7ba5](https://github.com/meshploy/meshploy/commit/e3b7ba5bdb2d60d758f8fb95e653ddeb89ed98c8))
* add inline route target editing and fix pod terminal shell prompt ([48201c7](https://github.com/meshploy/meshploy/commit/48201c7129533d19b906e331cb93c11e1cc49858))
* add IP rate limiting middleware for login and registration endpoints ([bd85660](https://github.com/meshploy/meshploy/commit/bd8566043635254c43108528a639e2d9599d8d77))
* add job management functionality ([e00e053](https://github.com/meshploy/meshploy/commit/e00e0532d543b6fe0e8e00c0709c3d3efcde6e9d))
* add live pod-level CPU/memory monitoring ([a96dde4](https://github.com/meshploy/meshploy/commit/a96dde435abc7769e5937e6a16d52ef1b3f192f0))
* add MCP server for AI tool integration ([f4ced42](https://github.com/meshploy/meshploy/commit/f4ced422239d0ddd1a7ec35fa832125acf0f0d8b))
* add mesh role management for nodes ([a6956ac](https://github.com/meshploy/meshploy/commit/a6956ac4b2641589ae38d0b2853527bae19331e1))
* add netavark and aardvark-dns to Dockerfile; update build context for nixpacks and railpack in meshploy-build; disable response buffering for SSE logs in Caddyfile ([50e772c](https://github.com/meshploy/meshploy/commit/50e772c9abb40b393f1a29916ab1e7a39a509b71))
* add new integration page for Git and registry providers ([389df72](https://github.com/meshploy/meshploy/commit/389df72578bfbf132e1471ae2c5b0145c418cd6c))
* add new project creation page and update routing for projects ([ac5df37](https://github.com/meshploy/meshploy/commit/ac5df37229654e285986913dde8e1bae737a2a1b))
* add new route for integrations creation and update imports in routeTree ([0c3579f](https://github.com/meshploy/meshploy/commit/0c3579f64f81548b543966f6e34ce4965ebb35b2))
* add node metrics functionality with node_exporter integration ([452fe32](https://github.com/meshploy/meshploy/commit/452fe32537dc5ca507a2ccb483d6653c0d47592a))
* add node selection and resource limits to stack services in editor ([94e4c69](https://github.com/meshploy/meshploy/commit/94e4c6954fc51bb3e932470af076c489d02cd2c9))
* add NodeTerminal component for interactive terminal sessions and integrate into routing ([b44b27f](https://github.com/meshploy/meshploy/commit/b44b27fcba28e3a20fa478d54df0e90b61e02345))
* add notification channels management with CRUD operations and UI integration ([8a70476](https://github.com/meshploy/meshploy/commit/8a704764ee026ff9516f3afd38e8cdad3776c03d))
* add passt to Dockerfile dependencies ([8ab85ed](https://github.com/meshploy/meshploy/commit/8ab85ed08fc878120884563a7193d7009362bff1))
* add pods tab with per-pod service terminal ([9bef5ae](https://github.com/meshploy/meshploy/commit/9bef5aec946ff6e4fd25883f6bed85462c526a6b))
* add Port field to service and workload models; update related logic for dynamic port handling ([a9a2145](https://github.com/meshploy/meshploy/commit/a9a2145eb1f9eba4b3bef4b855f997f75df5a2b3))
* add ports management in service config and UI ports display update ([9e7f8d3](https://github.com/meshploy/meshploy/commit/9e7f8d3dce3302175cfdc437467b9f3d9c3bb564))
* add project and service routes with layouts and tabs for navigation ([0bd2534](https://github.com/meshploy/meshploy/commit/0bd253443e0ed34f22b5f82499ccc3b30fbc60d3))
* add project management routes and components ([1665a2d](https://github.com/meshploy/meshploy/commit/1665a2d4c1eb90cc38c519776401392bd72ff194))
* add project, route, secret, and service management commands ([d73a589](https://github.com/meshploy/meshploy/commit/d73a589bf5926ca93401d5e5b1be5ce6d1f449b2))
* add PublicIP field to Config and Node models; enhance route creation with NodeID and Port ([5b044db](https://github.com/meshploy/meshploy/commit/5b044db43a82daf408e3e2f7d4d221dce67b05ea))
* add recharts metrics tab with sparkline upgrades on node detail ([48d8aae](https://github.com/meshploy/meshploy/commit/48d8aae76149418ebf36b544b5cff7f46e8fa438))
* add redirect route target type with multi-hop chain prevention ([91d386c](https://github.com/meshploy/meshploy/commit/91d386c43a9d0923dfdaca025653c0619e3c1412))
* add redirect target option to new route creation form ([58007e9](https://github.com/meshploy/meshploy/commit/58007e96c0a649c31221b513d6f5ccb3640d9ae0))
* add redirect URI support for GitLab and Gitea OAuth integrations ([6ae51ed](https://github.com/meshploy/meshploy/commit/6ae51ed24c4b7bbd87906dd07e643adac52a5f12))
* add registry integration functionality ([6005688](https://github.com/meshploy/meshploy/commit/6005688c1d6fc81916274167700846971fcb3a78))
* add rollback feature ([c654bb2](https://github.com/meshploy/meshploy/commit/c654bb2dcd431061cc297e960f71e9f7e7c50523))
* add route detail page and navigation; implement route fetching and deletion logic ([475e545](https://github.com/meshploy/meshploy/commit/475e54579fccd9f3222771f26437cb74e74d0f91))
* add routes index page and enhance service selection with port display ([f7bf2d0](https://github.com/meshploy/meshploy/commit/f7bf2d0a67036ee351cc998a6426002ce6d8a7c7))
* add secret management functionality ([192e6ed](https://github.com/meshploy/meshploy/commit/192e6ede74cfe57f1d69711be6def61c254b4477))
* add secrets route and update project layout to link to it ([193ba88](https://github.com/meshploy/meshploy/commit/193ba8822d7b07b1ec1f2edb0919f9a4a46382f1))
* add service overview route and enhance navigation with overview tab ([887b2ad](https://github.com/meshploy/meshploy/commit/887b2ad61da0f8312544dc4dde7b343a403f80dd))
* add service port editing, route detail page, and UI consistency pass ([54ffac4](https://github.com/meshploy/meshploy/commit/54ffac47afa20bfecd74b14b14bcbcaf659832bc))
* add service port selection in the route target configuration ([3702a00](https://github.com/meshploy/meshploy/commit/3702a0051b970126b8d746b02469d800c82e9c41))
* add stacks management to project layout ([056528f](https://github.com/meshploy/meshploy/commit/056528fdc4b942f44c361ef94efc2b16f3aba025))
* add storage integration functionality ([aad7879](https://github.com/meshploy/meshploy/commit/aad7879079653ecc28d780cbabf5ee0d0aae052d))
* add support for Docker bridge gateway IP for node metrics ([1022a23](https://github.com/meshploy/meshploy/commit/1022a23f048d098f7d70f9cb98048221436e0353))
* add support for groups in GitLab and Gitea integrations to scope repository access ([c6a03c3](https://github.com/meshploy/meshploy/commit/c6a03c3cf530157f3fa8c474b5e0c2d6778c7140))
* add support for insecure HTTP registries and enhance buildah caching options ([0cba02a](https://github.com/meshploy/meshploy/commit/0cba02a4947ade8c49ab5592232c543eb53548fb))
* add support for organization-specific GitHub app installation and improve dialog UI ([6f065be](https://github.com/meshploy/meshploy/commit/6f065bec9be39255a85f311840ecb3051dc0230d))
* add support for private image deployments ([8d5aa44](https://github.com/meshploy/meshploy/commit/8d5aa448c120865e36982c9f52a6fe6646baf6b5))
* add sync route IP functionality and update service navigation ([b332b13](https://github.com/meshploy/meshploy/commit/b332b13cf96e6e090a6a6a7b1c75d2a34862be8f))
* add timestamps to log output and improve log line parsing ([2bff909](https://github.com/meshploy/meshploy/commit/2bff909f08c8ebdc8feeb03772e36a3ebc5be2eb))
* add two-factor authentication support and update flow in CLI ([6dcb316](https://github.com/meshploy/meshploy/commit/6dcb31642c198cc7317b8446a43f1cfd10a65824))
* add variable and variable groups functionality ([81c2db7](https://github.com/meshploy/meshploy/commit/81c2db78144830b83ae80d6c6bed7d985c9115ae))
* add version tracking and update-available indicator in sidebar ([4a97ff5](https://github.com/meshploy/meshploy/commit/4a97ff539c8b8f209df1035caf2719140317ad0e))
* add volume backup management functionality ([eebdf69](https://github.com/meshploy/meshploy/commit/eebdf6907d8c6c254d227f5b6c85da36e9618eb7))
* add volume management functionality ([136463a](https://github.com/meshploy/meshploy/commit/136463a13cde59c62f0e23c37a1947d5300af903))
* auto-attach service system group to itself, block detach, exclude from own dropdown and enhance UI styles ([faf5fbc](https://github.com/meshploy/meshploy/commit/faf5fbc8ffcca1b07152993da3c9d2bf0f5edef9))
* **cli:** add integration management commands ([adf031d](https://github.com/meshploy/meshploy/commit/adf031d9d7862a6c0414e9458d4aa141dfeacc70))
* **cli:** add uninstall script handling and command for node removal ([1101759](https://github.com/meshploy/meshploy/commit/1101759843fc1d4c0874a4a8e8fd4c18fa456e69))
* **cli:** implement automatic fetching of preauth key and k3s join token ([1c37c20](https://github.com/meshploy/meshploy/commit/1c37c20e5c65cfd5b7458619aee550fe8f9175da))
* **cli:** implement install script download and serve endpoint ([e662090](https://github.com/meshploy/meshploy/commit/e662090915a6af5d1999d574ff08af7ee834a71f))
* conditionally render provision button based on service status ([ec21494](https://github.com/meshploy/meshploy/commit/ec21494d5b0c903a3e03977e35981f47010fe3f1))
* convert job detail to sub-routes, matching stack tab pattern ([f23f8e5](https://github.com/meshploy/meshploy/commit/f23f8e536739b5faf275a36ff30a16f253c2961b))
* **database:** add support for Dragonfly and ClickHouse database engines ([3c9f966](https://github.com/meshploy/meshploy/commit/3c9f96618c983e343f694923c15447fdbe302b90))
* **database:** enhance database pod slug handling and improve node selection logic ([d030415](https://github.com/meshploy/meshploy/commit/d030415a288d8ca82f14a09219d3db829bcda92f))
* **database:** enhance DBExplorerService to support Kubernetes port-forwarding and fetch pod details ([c22af10](https://github.com/meshploy/meshploy/commit/c22af10704d4521afbaa568644c1769d8daa9f61))
* **database:** implement DB Explorer with schema introspection and query execution ([3c5c8e7](https://github.com/meshploy/meshploy/commit/3c5c8e72bddd089388b9e716b46831cdb6495140))
* **database:** implement managed database provisioning and configuration ([0dbadab](https://github.com/meshploy/meshploy/commit/0dbadab88fa78e7ad2d1a2ce30d4332ea28be245))
* **database:** refine loadConfig to select online k3s nodes for connectivity ([a05c69c](https://github.com/meshploy/meshploy/commit/a05c69ca311b7e6283b64366097b15d399448c4b))
* **database:** update loadConfig to return NodePort and address for online nodes ([039adcc](https://github.com/meshploy/meshploy/commit/039adcc09d9dafdf4108c578bb438c306c986f46))
* Enhance build workflow to detect changed paths and update job dependencies ([0beb502](https://github.com/meshploy/meshploy/commit/0beb50206728adddb33618d06f720ab627f7b5f9))
* enhance deployment logs page with breadcrumb navigation and real-time timestamp updates ([0d3b570](https://github.com/meshploy/meshploy/commit/0d3b570932a6d7c607338cfcc935e2462b957dd3))
* enhance DNS configuration for build jobs to improve external hostname resolution ([0aedd97](https://github.com/meshploy/meshploy/commit/0aedd9760e32089492bbac3d57c012d60e14dd31))
* enhance Git integration with icons and improve copy functionality in service overview ([d1b0052](https://github.com/meshploy/meshploy/commit/d1b00522d6697583e7245188524fe832fb6163fa))
* enhance GitHub App manifest setup to support organization-specific URLs and add organization toggle in UI ([4053915](https://github.com/meshploy/meshploy/commit/4053915ebbc63efdbab37d61d737917f846801de))
* enhance GitHub installation URL handling to support organization-specific installation and improve integration flow ([ac7d740](https://github.com/meshploy/meshploy/commit/ac7d7401987317cace4ccd1a82eaa6cfb065c296))
* enhance Headscale URL handling and update preauth key panel messaging ([0ad7622](https://github.com/meshploy/meshploy/commit/0ad7622a0b4b8acec86c547466097619475bff84))
* Enhance installation and uninstallation scripts for improved functionality and user experience ([04490dc](https://github.com/meshploy/meshploy/commit/04490dc9aff0c6f8e28774064b9232725e0c061f))
* enhance job details configuration management ([a86713a](https://github.com/meshploy/meshploy/commit/a86713a8a89e3b9ee80c27168952716009320dbb))
* enhance log streaming and installation scripts with node-ip support ([35d5ecd](https://github.com/meshploy/meshploy/commit/35d5ecdf6788890f2f86289d937a5d8ea5e5dfa4))
* enhance log streaming with retry attempts and update Nixpacks build process to eliminate Docker daemon dependency ([896f2bc](https://github.com/meshploy/meshploy/commit/896f2bcbd3a55c5cb7c4da61cda7ae325b6f8edf))
* enhance node detail page with Headscale and k3s integration ([77bfb84](https://github.com/meshploy/meshploy/commit/77bfb843d07942ff556711b12b80666c54151396))
* enhance project and service detail navigation with service query integration ([4824af2](https://github.com/meshploy/meshploy/commit/4824af2607f5152a82ac2e783a692717469cf2e3))
* enhance project details with counts and improve routes display ([3d78ddb](https://github.com/meshploy/meshploy/commit/3d78ddb71edd267746b2d01edf9b850042bb77f8))
* enhance project listing with resource counts and update UI components ([1255c86](https://github.com/meshploy/meshploy/commit/1255c860b5f27f0fe3cb9932226fc05cb900426a))
* enhance project overview with project color coding and improve project card layout ([a3811e2](https://github.com/meshploy/meshploy/commit/a3811e24bc06a7ac7538b71637d457e6986cb0e8))
* enhance route creation to resolve K8s node IP via InternalIP addresses with fallback to name match ([00936a5](https://github.com/meshploy/meshploy/commit/00936a5ded1ba0871d12bc312ba597da9ee3d734))
* enhance RouteService to support Kubernetes integration for dynamic IP resolution ([653b89e](https://github.com/meshploy/meshploy/commit/653b89e39572f774577fbccb006c22cb12dfe882))
* enhance service creation and configuration with builder node and resource requests ([80f8f85](https://github.com/meshploy/meshploy/commit/80f8f852e30367e610f202eae3bc1625f473898d))
* enhance service logs functionality with streaming and download options ([8f52fa7](https://github.com/meshploy/meshploy/commit/8f52fa74fffeaef9a1ea62bb9c298eae33ace380))
* enhance service selection display with selected service name and port ([4dec845](https://github.com/meshploy/meshploy/commit/4dec8457707e8d28e8de34e449eb899566c36e0d))
* enhance sidebar with sticky positioning and height adjustment in new integration and project pages ([d6405cb](https://github.com/meshploy/meshploy/commit/d6405cb8892ddcbfe729ed46f5f19fa9aa11e894))
* enhance stack editor with database deployment options and conversion functionality ([7aeaeb7](https://github.com/meshploy/meshploy/commit/7aeaeb7745831ee59b2b6f5b4b11345cc9dce992))
* enhance stack editor with database service support and improved YAML handling ([9d35b99](https://github.com/meshploy/meshploy/commit/9d35b99bbc5c523877dcb4edfb8f7fc84455fa75))
* enhance SyncRouteIP to resolve target IP for auto-scheduled nodes ([9da3814](https://github.com/meshploy/meshploy/commit/9da38141c0594423b754de51f37b62e5bd39879e))
* enhance Variables Group Card component layout and functionality ([478c5d3](https://github.com/meshploy/meshploy/commit/478c5d30b3c2bd60cde70ac83bb783b50c60da34))
* extract SourceFields component and wire public/private source visibility in new service form ([77666f3](https://github.com/meshploy/meshploy/commit/77666f3605540e2d843d0448ce62077f66531c5d))
* implement backup execution engine with cron scheduler and retention reaper ([6ed8024](https://github.com/meshploy/meshploy/commit/6ed8024e320b35243f50cc61fe6e4ef17cfe1f76))
* implement custom domain verification for On-Demand TLS with Caddy integration ([35ba4bf](https://github.com/meshploy/meshploy/commit/35ba4bf99950543ef2188c4ce4b4ede6204530a9))
* implement custom select component for provider selection in registry dialog ([2654383](https://github.com/meshploy/meshploy/commit/26543830c133f8698fae5ab64d2b75b32d65962c))
* implement device trust functionality for enhanced security in login process ([9aabf1b](https://github.com/meshploy/meshploy/commit/9aabf1bc3078c40250da312d92a6ec5389a44318))
* implement GitHub App config reset functionality and remove domain deletion routes ([636755b](https://github.com/meshploy/meshploy/commit/636755be5d8f0227effca86af3df8e69a5a47389))
* implement Headscale ID management for nodes and enhance deletion logic ([954250f](https://github.com/meshploy/meshploy/commit/954250fce57b9081fb30d0f20851756eb1f57d33))
* implement Headscale preauth key storage and retrieval with encryption ([560ee3c](https://github.com/meshploy/meshploy/commit/560ee3c7b15961d694ed17f86d4b496247bccd22))
* implement job detail page ([3608cac](https://github.com/meshploy/meshploy/commit/3608cac81ed133634e64e998f4f2e2cea77cb36d))
* implement job execution logic and integrated into the UI ([01a6183](https://github.com/meshploy/meshploy/commit/01a6183d7a02e918b436d7a3d80c0ca295b7b76a))
* implement member management features including listing, inviting, and updating roles ([56fa10c](https://github.com/meshploy/meshploy/commit/56fa10c5847ca204ec390e72daabd374419a1871))
* Implement Meshploy installation and configuration scripts ([98a61bb](https://github.com/meshploy/meshploy/commit/98a61bbd0ef0940d069878d86c54f882314e2b23))
* implement metrics store for managing node metrics history ([0dcf3b8](https://github.com/meshploy/meshploy/commit/0dcf3b8485ba934fc132f166961bad98d1e11fa6))
* implement org membership enforcement middleware ([d048959](https://github.com/meshploy/meshploy/commit/d04895990c6aa6a4a2a189f82f588a1278915cdf))
* implement per-node provisioning tokens and enhance auth middleware ([460058a](https://github.com/meshploy/meshploy/commit/460058a193e25187cc624f8060ede010cd611eed))
* implement retrieval of the most recent Headscale preauth key and update related UI components ([e70574b](https://github.com/meshploy/meshploy/commit/e70574bf2996422d5c0e860ffe54d0beae76b693))
* implement ScaleDeployment function and integrate with WorkloadService for service start/stop ([1247f2a](https://github.com/meshploy/meshploy/commit/1247f2a6fb7f5f618648c3e8486cb633084b9a84))
* implement self-deregistration for nodes and enhance uninstall script ([c5a4009](https://github.com/meshploy/meshploy/commit/c5a40092c0f14e9e4072e99b3b3b3ca79334f509))
* implement service start/stop functionality and enhance route update logic ([02dc851](https://github.com/meshploy/meshploy/commit/02dc851e50f880791c8d780549afefb21ac7d793))
* implement single-service deployment flow with unified creation page ([0c31569](https://github.com/meshploy/meshploy/commit/0c31569385e4a65395566adb420cb54899a20f87))
* implement subdomain validation and error handling in route forms ([15694ac](https://github.com/meshploy/meshploy/commit/15694ac7ad1093cd9b8750d40b13d30988513839))
* implement tab management with TabBar component and session handling ([f2c315f](https://github.com/meshploy/meshploy/commit/f2c315f34275e45cb6f57b4cbfd05d140f6ef57f))
* implement two-factor authentication (TOTP) support ([b1933d5](https://github.com/meshploy/meshploy/commit/b1933d522bca1d6002bd10f1b3d07a41291ad85e))
* implement user ID resolution for Headscale preauth key creation ([fbb51ba](https://github.com/meshploy/meshploy/commit/fbb51ba918b3d2b427c5a85ecad69509410e8a1c))
* improve log streaming reliability and update Dockerfile for Alpine base ([2b3718b](https://github.com/meshploy/meshploy/commit/2b3718b68c3a9d560d644fcc8be5d319c15ca7d6))
* improved theming functionality with accent store and fonts ([8a9b238](https://github.com/meshploy/meshploy/commit/8a9b238635bebd686ad1050781f316c93032220e))
* integrate DB Explorer component into the tabbed layout ([96d8edf](https://github.com/meshploy/meshploy/commit/96d8edf3d36452726826caa74457b45869c79c4a))
* integrate notification service for backup and deployment events; add support for Slack and Discord channels ([2be8670](https://github.com/meshploy/meshploy/commit/2be8670fd99c1e01b4a020a870e9675b1842662a))
* integrated a visual editor for YAML ([c771a01](https://github.com/meshploy/meshploy/commit/c771a014a961c6843c2b11fae7412a79b5eec61c))
* make workload patch input fields optional for improved flexibility ([0ba12a6](https://github.com/meshploy/meshploy/commit/0ba12a65e98ff6eb81f2e60ca77957d36aa27a43))
* move variable group creation to new resource page ([0f14fbd](https://github.com/meshploy/meshploy/commit/0f14fbd60677109095667f5ad16b447725bce131))
* **overview:** add database connection details and enhance service overview UI ([7740aa3](https://github.com/meshploy/meshploy/commit/7740aa3ba316a76225d11a50e35f4555f351d8fe))
* pre-seed railpack's mise cache with musl binary to avoid glibc download issues ([c5b7662](https://github.com/meshploy/meshploy/commit/c5b7662d3184c6e1a7fef2315ae4803789442988))
* read cluster join token from filesystem for up-to-date access ([09a660d](https://github.com/meshploy/meshploy/commit/09a660da86f19d94c676b21e4a85d9aa16048eac))
* Refactor installation script phases and add uninstall script for complete management ([091ecc2](https://github.com/meshploy/meshploy/commit/091ecc231c967766e1957d28c28725fb248a8a9d))
* refactor service configuration forms and extract common components for better reusability ([add98c8](https://github.com/meshploy/meshploy/commit/add98c8ec525a3c2d149d639d17c6d191c0305c1))
* refactor service port handling to support multiple ports ([84da990](https://github.com/meshploy/meshploy/commit/84da990e3159c349f1208c1a8d56bdb639913fe8))
* remove darwin/arm64 build configuration from CLI workflow ([af15d10](https://github.com/meshploy/meshploy/commit/af15d10e0508eb25b97353317dc960245e159758))
* remove secrets functionality; Secrets are fully superseded by variable groups ([1835aff](https://github.com/meshploy/meshploy/commit/1835aff43ca3df853215c55a693bfde0922e4d54))
* replace hand-rolled compose parser with compose-go/v2; add stack variables support ([72913c5](https://github.com/meshploy/meshploy/commit/72913c50edc3d8110c34dc5fcb12110166a15c45))
* replace single-target routes with path-based RouteTarget join table and longest-prefix proxy matching ([a989d30](https://github.com/meshploy/meshploy/commit/a989d30a646e78d90fdeff06ac732762742866bc))
* route service targets through K8s NodePort and remove manual sync ([af8a856](https://github.com/meshploy/meshploy/commit/af8a856a3fd3956c864e94504884686453c1741f))
* service source matrix - public git, private image pull secrets, direct image deploy ([c7a5f2e](https://github.com/meshploy/meshploy/commit/c7a5f2ecdba4979112f5f996d2ec22a4eaafe387))
* update cache-from reference in build script to use bare repository format ([2e4ffa5](https://github.com/meshploy/meshploy/commit/2e4ffa51f9c27d89a486e43c10fc785a3b207b58))
* update Dockerfile to use Alpine 3.22 and refactor railpack build script for improved environment variable handling ([3b7bbaa](https://github.com/meshploy/meshploy/commit/3b7bbaaf5811682e8573a4b608f81590e82a1da5))
* Update Dockerfiles to improve module dependency management and add vite-env type reference ([b20a5c7](https://github.com/meshploy/meshploy/commit/b20a5c7544c1963835c21f66c61dccc83fb50acd))
* update Dockerfiles to include CLI module dependencies and modify get.sh for specific CLI release tagging ([6f1eef5](https://github.com/meshploy/meshploy/commit/6f1eef5ebb96af72f3a9d45d24657295d2d0762a))
* update domain setup wizard and settings page to enhance domain management experience ([13bea87](https://github.com/meshploy/meshploy/commit/13bea87a23d28ce7996f3c421e56475bdfa737f3))
* update Headscale preauth key panel to display server URL and improve key management instructions ([b56ac9b](https://github.com/meshploy/meshploy/commit/b56ac9b855ceb6402b5f87beaff2ecebe7c735e2))
* update Meshploy API configuration and improve node registration logic ([e8258a2](https://github.com/meshploy/meshploy/commit/e8258a26e2154c0b6fb22ddb7509254e091bca94))
* update Nixpacks and Railpack build processes to use buildah for Dockerfile generation ([32ad4be](https://github.com/meshploy/meshploy/commit/32ad4be734418637e309792f8f621d49f6b968e7))
* update Nixpacks and Railpack build processes to use output directories for Dockerfile generation ([0e96f74](https://github.com/meshploy/meshploy/commit/0e96f748ee363955a75838ab7db5b4096dab9708))
* update node selection logic for database services in ServiceOverviewTab ([2c81662](https://github.com/meshploy/meshploy/commit/2c81662e05758803a9567e74a99e1210fd1ea585))
* update node_exporter service configuration to allow access from container bridge networks ([9b247d0](https://github.com/meshploy/meshploy/commit/9b247d05bbe9780dce43260305705f63d625e60b))
* update NodeDetailPage cards display ([305cdfa](https://github.com/meshploy/meshploy/commit/305cdfaec99c7b187220e2e9e5f5fd0ddc96d268))
* update route and service creation to include search type; refactor modal handling ([e4387f3](https://github.com/meshploy/meshploy/commit/e4387f3376adf5ccc64787d25582627db688eff0))
* update Switch styles and variable group management UI ([49be098](https://github.com/meshploy/meshploy/commit/49be098f943019cbf5093395d6aef6bb1efe13ca))


### Bug Fixes

* add groups field to git integration mutation type annotations ([4fcd7cd](https://github.com/meshploy/meshploy/commit/4fcd7cd56666ac7fec0cebad530cef78d9db57a3))
* add Link import in ProjectLayout ([c6d954f](https://github.com/meshploy/meshploy/commit/c6d954f5c466d4e4bcad506334e0fd47a97bbf57))
* anchor percentage sparklines and metrics charts to 0–100% domain ([841002d](https://github.com/meshploy/meshploy/commit/841002dc0b8925b7a4cd7fb7945f3a41a81a6436))
* auto-refresh OAuth tokens and add MIT license ([8b46c2d](https://github.com/meshploy/meshploy/commit/8b46c2da30f318fe96f5d703779420f43105406e))
* change update method from PUT to PATCH for node updates ([7f696e0](https://github.com/meshploy/meshploy/commit/7f696e0037ace38285548f001b7835f5036082c5))
* **cli:** cli update path ([b998a0a](https://github.com/meshploy/meshploy/commit/b998a0a09cb4ae92f12c86d21d061418dbd099f7))
* **cli:** set TERM environment variable ([8f17cee](https://github.com/meshploy/meshploy/commit/8f17ceecb9ccd34f529090c23f68a91e0d4a4bbb))
* **cli:** update API endpoint for registration token retrieval ([da32031](https://github.com/meshploy/meshploy/commit/da320317b7c450410e658ecaaddd268aec6c7213))
* **cli:** update SSH command execution for adding node ([3ea51c9](https://github.com/meshploy/meshploy/commit/3ea51c93d281be2faad16ff89c2fbc2025bd4652))
* **deploy:** improve config file check and guidance for direct script execution ([4f61222](https://github.com/meshploy/meshploy/commit/4f61222ed1803bda14b21ce9645f870b7fef4ba0))
* display selected provider label in Storage and Registry forms ([db33a23](https://github.com/meshploy/meshploy/commit/db33a23b9235370226484a306d6bafc04f890ff1))
* enhance hover styles for buttons and service terminal style ([844f165](https://github.com/meshploy/meshploy/commit/844f165e20eda4a8508b6e73f77b7bed0059c08d))
* ensure proper cancellation of exec context on WebSocket closure in NodeTerminal ([41abef8](https://github.com/meshploy/meshploy/commit/41abef87f8edd02c66dc6e7ea3f3d47fa52e995c))
* ensure storage selection updates only on valid value change ([7f9f4fb](https://github.com/meshploy/meshploy/commit/7f9f4fb50595322273cdb466de4dac1ded24440c))
* fix job page stylings and added codemirror support for multi line text inputs ([a3ee0b9](https://github.com/meshploy/meshploy/commit/a3ee0b9cfd65d9ae795285cbc8f13459b26b8a5a))
* fixed buildctl installation ([a77f535](https://github.com/meshploy/meshploy/commit/a77f535520a9df8b79b2e3a7ef9f7d1bc9f5b4ad))
* handle empty result columns and rows in ResultsTable component ([976e220](https://github.com/meshploy/meshploy/commit/976e2207793e3ce151222b5b154d9d09c8ec0da1))
* improve container runtime detection in deployment script ([3c2471f](https://github.com/meshploy/meshploy/commit/3c2471ffc3804f4b9cb1498939f928ebf03fa20d))
* Improve error handling for Headscale key generation in installation script ([41f7b10](https://github.com/meshploy/meshploy/commit/41f7b10f49144464c40716934a504b0e871cc0b5))
* Improve uninstall script to handle missing deploy directory and enhance confirmation prompt ([8fbf63d](https://github.com/meshploy/meshploy/commit/8fbf63d40989e521d7f21e506f4ba8ab480165ce))
* initialize prevUpdatedAt with the timestamp of the last history entry in NodeDetailPage ([528e675](https://github.com/meshploy/meshploy/commit/528e675a81b5984d6f81ee57842cab648728e32c))
* initialize slices with make to avoid nil slice issues in service list methods ([9106c21](https://github.com/meshploy/meshploy/commit/9106c210bd7f48f2e89d69bf7756366516677582))
* k3s installation process in the deployment script ([fe9b6cd](https://github.com/meshploy/meshploy/commit/fe9b6cd12c09f48b6a051afce2519d936d2622cf))
* normalize form heading style across project and integrations new pages ([f65776e](https://github.com/meshploy/meshploy/commit/f65776e15c6f46367f3c9bb00f9ce77cc0ece013))
* re-fetch node after updating role; round numerical stats ([9036c41](https://github.com/meshploy/meshploy/commit/9036c41eb85da04286beb8a92eaf01a955f641f8))
* re-provision button for databases and back label for database detail page ([3d3be6d](https://github.com/meshploy/meshploy/commit/3d3be6dcc9fdd649151a87bcef41101e715ae256))
* Refactor repository URL handling in installation script ([07aa21c](https://github.com/meshploy/meshploy/commit/07aa21cd50b783bd5d8003fdd9b5f562bd5f72ce))
* reference EMT metrics history in NodeDetailPage ([2f8c24a](https://github.com/meshploy/meshploy/commit/2f8c24aefc125c90a9233a0ebbc7f253eb216fab))
* regenerate route tree for job sub-routes ([7b712a9](https://github.com/meshploy/meshploy/commit/7b712a9de2a7ebf31abaae843bcffab122550517))
* remove hint from Secret field in NotificationsForm ([55196c2](https://github.com/meshploy/meshploy/commit/55196c2c5146704fcfbc9b98c6c2f60c0856252d))
* remove nixpacks, fix tab border bleed, and align stack tabs to router ([8df72c2](https://github.com/meshploy/meshploy/commit/8df72c2c9462ac8831fd2e13bd86e270cbf871e4))
* remove stale Service.Routes association after RouteTarget migration ([2c05b84](https://github.com/meshploy/meshploy/commit/2c05b8469bb42bafcfb850d4bba93c4c909be3d7))
* remove stale useRouterState call in stack layout ([4de749c](https://github.com/meshploy/meshploy/commit/4de749c1fd85acada3a86b67b62b0758c1c67ebf))
* Remove unnecessary Docker config redirection in installation script ([f6d2db5](https://github.com/meshploy/meshploy/commit/f6d2db59aa7494da25e311d34b41463354e102b5))
* remove unnecessary interval and burst settings from On-Demand TLS configuration ([688ee90](https://github.com/meshploy/meshploy/commit/688ee901684261621ae06c5cdd4e55b0cad6f9fe))
* replace Link component with navigate for New Service button actions ([78c7ce9](https://github.com/meshploy/meshploy/commit/78c7ce95c5139259fbfc9f7d7b9e86382220d060))
* resource counting ([6da73c0](https://github.com/meshploy/meshploy/commit/6da73c06ce01366079e596e50afbb92ad3e75864))
* route tree generation ([0b376fd](https://github.com/meshploy/meshploy/commit/0b376fde579f3631a4ebe9e8b49b0bfc87ede29a))
* simplify refetch interval logic for service query ([21c3880](https://github.com/meshploy/meshploy/commit/21c3880b03c1b17b4a2d58c0ced0f7f102b844de))
* Update .gitignore and add ACME challenge zone template for DNS ([24eef30](https://github.com/meshploy/meshploy/commit/24eef30920167ac49cf2738d2673044ad14dd3b3))
* update base URL to use FrontendURL for proper GitHub callbacks ([81089b2](https://github.com/meshploy/meshploy/commit/81089b2c65377838616103e100bd028e1c8a4e06))
* Update Caddyfile to use handle_path for API routing ([1582c0e](https://github.com/meshploy/meshploy/commit/1582c0e47a368386142cf3c963c2480e47e6b841))
* update color for active button state ([c844572](https://github.com/meshploy/meshploy/commit/c8445720c33e0d90cf5009a51ffa9b641826ad32))
* update components for consistent styling ([51f7859](https://github.com/meshploy/meshploy/commit/51f78598e245cc98680d32d183e941711ddbe0ea))
* Update Docker configuration and connection method for Headscale in installation script ([48ab6be](https://github.com/meshploy/meshploy/commit/48ab6be2092bcf38df4a46a03269295feb3f5065))
* update GitHub installation URL for correct redirect and enhance installation instructions ([1b125d3](https://github.com/meshploy/meshploy/commit/1b125d32666472c7810fd6e9b59b68e894589e4c))
* update GitHub installation URL to correct format ([e6a7c40](https://github.com/meshploy/meshploy/commit/e6a7c403e7e93a800d1fb565e630d198812b727b))
* Update Headscale image version and adjust user ID handling in installation script ([d57a8a5](https://github.com/meshploy/meshploy/commit/d57a8a58c3eb9dd33b84e64a002e0d3234072534))
* Update Headscale image version and improve error handling in installation script ([7f5f0a1](https://github.com/meshploy/meshploy/commit/7f5f0a157efa8dcec01a078ab54102c2a28606e3))
* Update Headscale user ID extraction to parse from plain-text output for reliability ([b6080bb](https://github.com/meshploy/meshploy/commit/b6080bbc9729164d44ce8b591e53c4c4daa90d3a))
* Update headscale user ID extraction to support multiple JSON formats ([f552249](https://github.com/meshploy/meshploy/commit/f5522499c1aee3a358ed9ace5861fa0e3bf665f8))
* Update Headscale user ID handling to use numeric ID instead of username in installation script ([75959e6](https://github.com/meshploy/meshploy/commit/75959e6a57207542323ad19c7b4ea7ed24e643e9))
* update loading state handling in NodeDetailPage component ([0c4b8c7](https://github.com/meshploy/meshploy/commit/0c4b8c721a725cb54b8a689528d0787e1db32dbf))
* update logo in auth pages ([8f97073](https://github.com/meshploy/meshploy/commit/8f970730f74d6c94140635a51b9c9526e3fbbe4a))
* update navigate function to use a callback for search parameters ([31bf81a](https://github.com/meshploy/meshploy/commit/31bf81a851a55ceabc7a9719cdb0e70498b625f4))
* update navigate function with to property ([ca5be7c](https://github.com/meshploy/meshploy/commit/ca5be7c2d8be4c492a8b16105be3e35f6934dd62))
* update NodeTerminal command path and adjust terminal container styles ([2cb0490](https://github.com/meshploy/meshploy/commit/2cb049096834033aad638e28b487e8a7e0f4ad6e))
* Update Railpack installation method to use raw.githubusercontent.com for reliability in CI builds ([804cd25](https://github.com/meshploy/meshploy/commit/804cd25a3364f033d0aba4b7f5bab8b1ac738edf))
* Update regex for extracting pre-auth and API keys in installation script ([08ac8dd](https://github.com/meshploy/meshploy/commit/08ac8dd99fe823bf8b515b2a6e892c19056b992f))
* update Replicas field comment to clarify default behavior ([64e0b43](https://github.com/meshploy/meshploy/commit/64e0b43f646a7ab1cae5e1be2df3f43071481112))
* use API asset URL for private repo CLI downloads ([f9490c7](https://github.com/meshploy/meshploy/commit/f9490c7e76a73c3d9b2d998321eab1414bab4b25))
* **web:** enhance node status indication ([c4be50f](https://github.com/meshploy/meshploy/commit/c4be50fa4c72e1a189db8ec2b9284f6f63067cea))
