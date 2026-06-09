memory

the purpose of this project is to provide an mcp server that will act as a tribal knowledge context server. consider the team of financial analysts that have to evaluate stocks, the companies, earnings, etc. They use LLMs to help process the disparate data into reports for their management. each analyst prompts the llm differently. for some they get excellent reports with beautiful charts and excellent actionable signals. others do not have the same results. they prompt the LLM differently. because of these variances in approach the output is different across users. If users could share ideas, prompt style, and other useful tribal knowledge then all of the users will benefit from each others strong points. This is probably much more than just a memory mcp but a system of prompt improvement, learning what works and what doesn't and potentially on the fly improvement of a user prompt based on the context and memories and analysis of prompts from the other team members. 

## MCP Usage Instructions

This project is served by the tribal-knowledge MCP server. When the MCP server is connected:

**At the start of every request:** Call `enrich_context` with the user's message before planning or drafting a response. This pulls applicable team rules and relevant knowledge. Apply what it returns — improved_prompt, applicable_rules, and relevant_knowledge.

**After completing any non-trivial task:** Call `knowledge_store` to capture reusable learnings. Call `knowledge_use` and `knowledge_rate` on any entries you relied on.

**Learning loop:** The knowledge base improves through use — every rating and usage signal makes future retrieval better. Treat `knowledge_rate` as part of task completion, not optional.