// The greeting was written by the build step from a build variable, which
// Railpack passes as a BuildKit secret: so it says the secret reached the build.
const fs = require("fs")
const greeting = fs.existsSync("greeting.txt") ? fs.readFileSync("greeting.txt", "utf8") : "unset"
console.log(`railpack build says ${greeting}`)
