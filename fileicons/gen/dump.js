// Dumps file-icons' compiled icon table as JSON, regexes as source and flags.
const db = require(process.argv[2]);
const re = r => r instanceof RegExp ? {source: r.source, flags: r.flags} : null;
const [icons] = db[1]; // file icons; db[0] is directories
const out = icons.map(([icon, colour, match, priority = 1, matchPath = false]) =>
	({icon, colour, match: re(match), priority, matchPath: !!matchPath}));
process.stdout.write(JSON.stringify(out));
