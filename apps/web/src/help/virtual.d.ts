declare module "virtual:help-topics" {
  /** Each help topic's Markdown, by file name. */
  const topics: Record<string, string>
  export default topics
}
