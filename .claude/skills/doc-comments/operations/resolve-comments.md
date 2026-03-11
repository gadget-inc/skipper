# Resolve Comments

Apply each pending reviewer comment to the MDX source file it targets, delete the resolved JSON file, and report the outcome.

1. **Load comment list**: The injected listing below shows pending comment files:

   !`ls docs/tmp/comments/*.json 2>/dev/null | head -30 || echo "No pending comments."`

   If the output is `No pending comments.`, report that there is nothing to resolve and stop.

2. **Plan batches**: If more than 20 files are listed, process in batches of 10 — resolve one batch fully (through clean up) before starting the next. Files beyond the first 30 are handled in subsequent batches once earlier ones are cleared. Report between batches: "Batch N of M complete — X resolved, Y skipped."

3. **Map pages to source files**: For each comment, derive the MDX source path:
   - Strip the `/skipper/` prefix and trailing `/` from the `page` field
   - The source file is `docs/src/content/docs/<remainder>.mdx`
   - Example: `/skipper/guides/scaling/` -> `docs/src/content/docs/guides/scaling.mdx`
   - If the derived source file does not exist, skip the comment and include it in the final report

4. **Interpret each comment**: Read each comment file individually at the start of its processing. Comments are terse reviewer shorthand — invest effort in understanding what the reviewer means before asking the user:
   - `selectedText` tells you _where_ the reviewer was looking, not _what_ needs to change — the fix may be in surrounding text, a different paragraph in the same section, or a different section of the same page. Always expand context before proposing a fix: start with the `selectedText` location in the source file, then expand to the full section (from the nearest `##` heading above to the next `##` heading or EOF), then the full page if still ambiguous, then referenced pages if the comment mentions other pages. Note that `contextBefore`/`contextAfter` in the JSON are short snippets captured at comment time — use them only for locating selected text in step 5, not as sufficient context for interpretation.
   - MUST cross-reference claims against the actual codebase (e.g., check struct definitions, config defaults, flag names) to verify or disprove the selected text
   - Consider what would make the comment true — if a reviewer says something "is wrong," figure out _how_ it's wrong by checking the source of truth

5. **Confirm fix with user**: Present findings and proposed fix to the user via AskUserQuestion before making any edit — state what you found in the codebase, what you believe the reviewer means, and what change you plan to make, then ask for confirmation. Always include a freeform "Something else" option so the user can describe their preferred fix.
   - Default: ask about one comment at a time via AskUserQuestion. Exception: two or more comments on the same file MAY be grouped into a single AskUserQuestion only if no codebase lookup was required for any of them and each fix is a single text substitution
   - If the user rejects the proposed fix and suggests an alternative, adopt the user's direction — do not re-propose the original fix or variations of it. Confirm understanding of the user's alternative and proceed
   - If after investigation you cannot determine a proposed fix, present your findings and ask the user what they want changed

6. **Resolve each comment**: For each comment in turn (user has already confirmed the fix in step 5):
   - Read the source MDX file. When multiple comments target the same file, re-read the file before each edit to account for changes from prior edits in this run
   - Use `selectedText` with `contextBefore` and `contextAfter` to locate the exact text in the file
   - If `selectedText` cannot be found in the file, skip the comment and include it in the final report
   - The confirmed fix may target text other than `selectedText` — possibly elsewhere in the same file or in a different file. Apply the edit where the user confirmed, not necessarily where the comment was placed
   - Apply the confirmed fix
   - MUST follow the project's docs writing rules (loaded automatically via scoped rules for docs files)
   - Handle edits based on the `zone` field in the comment JSON:
     - **`content` / `title` zones**: edit the MDX source directly
     - **`toc` zone**: heading structure issues — edit heading levels or text as confirmed
     - **`sidebar` zone**: navigation config in `docs/astro.config.mjs` — edit as confirmed
   - Report progress after each comment: "Resolved N of M comments."

7. **Clean up**: After successfully resolving a comment, MUST delete its JSON file from `docs/tmp/comments/`. If the file is already gone (e.g., resolved by a parallel run), treat it as a no-op.

8. **Verify**: Run `direnv exec . docs build` after all edits. If the build fails, check the error for the referenced file, revert only that file's edit with `git restore <file>`, and re-run the build to confirm. Restore the comment file for any reverted edit.

9. **Report**: Summarize the outcome as a short bulleted list:
   - Number resolved
   - Number skipped, each with a reason (file not found, text not found, ambiguous)
   - Any comments still pending
