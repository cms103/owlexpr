# The owlexpr Standard Library

The core language — covered in [LANGUAGE.md](LANGUAGE.md) — is deliberately
small. Everything beyond it lives in a set of optional **packs** that your
application can choose to turn on. Each pack adds a group of functions under
its own name (a "namespace"), used with a dot, the same way you'd access a
field on a piece of data:

```
string.trim(name)
list.sort(scores)
```

Whether a pack is available depends on how the application you're using was
built — if `string.trim(...)` gives an "undefined" error, that pack simply
wasn't turned on for this expression field, not that you did something
wrong.

## Contents

1. [A few things that apply everywhere below](#a-few-things-that-apply-everywhere-below)
2. [string](#string) — working with text
3. [regex](#regex) — pulling pieces out of text with patterns
4. [list](#list) — working with lists
5. [time](#time) — dates and times
6. [decimal](#decimal) — exact numbers for money and similar values
7. [bytes](#bytes) — raw binary data
8. [iter](#iter) — working through very large amounts of data piece by piece
9. [json](#json) — converting to and from JSON text
10. [bits](#bits) — using a number as a set of on/off flags

## A few things that apply everywhere below

**Some functions produce values you can't type directly.** A handful of
functions below (`decimal(...)`, `time.now()`, and others) produce
values — an exact decimal amount, a point in time, a piece of raw binary
data — that don't have their own typing in an expression the way `42` or
`"hello"` do. You get them from your data, or by calling one of these
functions; you can still store them, compare them, and pass them to other
functions like any other value.

**Nothing here changes the list or map you pass in.** Every function that
works on a list or a map hands back a new one and leaves the original
exactly as it was.

**`list.*` is for data you already have; `iter.*` is for working through a
lot of it piece by piece.** If you're not processing something huge, you
want `list.*` — see the [iter](#iter) section for when the other one earns
its keep.

## string

Turned on with `StringBuiltins()`, used as `string.*`.

Everything here counts and measures actual letters, not raw computer bytes
— so an accented letter like "é" still counts as one character, the same
way it would if you counted it yourself.

| Function | What it does | Example |
|---|---|---|
| `trim(s)` / `trim(s, chars)` | Removes blank space from both ends, or every character in `chars` if you give one. | `trim("  hello  ")` → `"hello"` |
| `trimLeft(s)`, `trimRight(s)` | Same as `trim`, but only from the start or only from the end. | `trimLeft("  hello  ")` → `"hello  "` |
| `trimPrefix(s, prefix)` | Removes `prefix` from the front, if it's there. | `trimPrefix("hello.txt", "hello")` → `".txt"` |
| `trimSuffix(s, suffix)` | Removes `suffix` from the end, if it's there. | `trimSuffix("hello.txt", ".txt")` → `"hello"` |
| `upper(s)` | Makes every letter upper case. | `upper("héllo")` → `"HÉLLO"` |
| `lower(s)` | Makes every letter lower case. | `lower("HÉLLO")` → `"héllo"` |
| `split(s, sep)` | Breaks a string into a list of pieces wherever `sep` occurs. | `split("a,b,c", ",")` → `["a", "b", "c"]` |
| `splitAfter(s, sep)` | Same as `split`, but keeps `sep` attached to the piece before it. | `splitAfter("a,b,c", ",")` → `["a,", "b,", "c"]` |
| `replace(s, old, new)` | Replaces every occurrence of `old` with `new`. | `replace("foo bar foo", "foo", "baz")` → `"baz bar baz"` |
| `repeat(s, n)` | Repeats a string `n` times. | `repeat("ab", 3)` → `"ababab"` |
| `indexOf(s, sub)` | Where `sub` first appears in `s` (counting from 0), or `-1` if it's not there. | `indexOf("café bar", "bar")` → `5` |
| `lastIndexOf(s, sub)` | Same, but the last place it appears. | `lastIndexOf("abcabc", "a")` → `3` |
| `hasPrefix(s, p)` | Whether `s` starts with `p`. | `hasPrefix("hello.txt", "hello")` → `true` |
| `hasSuffix(s, p)` | Whether `s` ends with `p`. | `hasSuffix("hello.txt", ".txt")` → `true` |
| `contains(s, sub)` | Whether `sub` appears anywhere in `s`. | `contains("hello world", "wor")` → `true` |
| `count(s, sub)` | How many times `sub` appears in `s`. | `count("banana", "an")` → `2` |
| `join(list)` / `join(list, sep)` | Joins a list of strings into one string, with `sep` between each piece (nothing, if you don't give one). Every item in the list must already be a string. | `join(["a", "b", "c"], "-")` → `"a-b-c"` |
| `reverse(s)` | Reverses a string, character by character. | `reverse("héllo")` → `"olléh"` |
| `padStart(s, len)` / `padStart(s, len, pad)` | Adds characters to the front until `s` is `len` characters long (spaces, unless you give `pad`). | `padStart("é", 3, "x")` → `"xxé"` |
| `padEnd(s, len)` / `padEnd(s, len, pad)` | Same, but adds to the end. | `padEnd("7", 3, "0")` → `"700"` |
| `format(template, ...values)` | Fills a template with values, in the style of a common text-formatting pattern: `%s` for text, `%d` for whole numbers. | `format("%s is %d", "Bob", 42)` → `"Bob is 42"` |
| `toBase64(s)` / `fromBase64(s)` | Converts text to and from base64, a common way to encode text or data as plain letters and numbers so it's safe to put in places that don't allow arbitrary characters. | `toBase64("hi")` → `"aGk="` |

To test whether text matches a pattern, use the language's own `matches`
operator (see LANGUAGE.md). To pull pieces out of text with a pattern,
see [regex](#regex).

## regex

Turned on with `RegexBuiltins()`, used as `regex.*`.

A regular expression is a pattern describing some text — "one or more
digits", "a word, then an @, then another word". These functions use one
to find, pull out, replace or split on the parts of a string that match.

Every function takes the pattern first, written as a regex literal:
`` re`...` `` (see LANGUAGE.md). A pattern in an ordinary string won't work
here — `regex.find("\\d+", s)` is an error; write `` regex.find(re`\d+`, s) ``.
Because the pattern is part of the expression itself, a mistake in it is
reported as soon as the expression is saved, not later when it runs.

| Function | What it does | Example |
|---|---|---|
| `capture(re, s)` | The named pieces of the first match, as a map — name a piece with `(?P<name>...)`. `nil` if there's no match. | `` capture(re`(?P<user>\w+)@(?P<host>\w+)`, "me@example") `` → `{"user": "me", "host": "example"}` |
| `captureAll(re, s)` / `captureAll(re, s, n)` | `capture` for every match (at most `n`), as a list. | `` captureAll(re`(?P<k>\w+)=(?P<v>\w+)`, "a=1 b=2") `` → `[{"k": "a", "v": "1"}, {"k": "b", "v": "2"}]` |
| `groups(re, s)` | The first match, then each bracketed piece in order, as a list. `nil` if there's no match. | `` groups(re`(\w+)@(\w+)`, "me@example") `` → `["me@example", "me", "example"]` |
| `find(re, s)` | The first piece of text that matches, or `nil` if none does. | `` find(re`\d+`, "room 42b") `` → `"42"` |
| `findAll(re, s)` / `findAll(re, s, n)` | Every piece of text that matches (at most `n`), as a list — empty if none do. | `` findAll(re`\d+`, "1 and 22") `` → `["1", "22"]` |
| `replace(re, s, with)` | Replaces every match. In `with`, `$1` or `${name}` stands for a bracketed piece of the match (`$$` for a plain `$`). `with` can also be a function, given each match and returning its replacement. | `` replace(re`(\w+)@(\w+)`, "me@example", "$2/$1") `` → `"example/me"`; `` replace(re`[a-z]+`, "ab-cd", m => string.upper(m)) `` → `"AB-CD"` |
| `replaceLiteral(re, s, with)` | Same as `replace`, but `with` is used exactly as written — a `$` is just a `$`. | `` replaceLiteral(re`\d`, "a1", "$") `` → `"a$"` |
| `split(re, s)` / `split(re, s, n)` | Breaks a string into a list of pieces wherever the pattern matches (into at most `n` pieces). | `` split(re`\s*,\s*`, "a , b,c") `` → `["a", "b", "c"]` |
| `pattern(re)` | The pattern's text. | `` pattern(re`\d+`) `` → `"\d+"` |

A piece of the pattern that's optional and didn't match comes back as
`nil`, not as empty text — in `` capture(re`(?P<sign>-)?(?P<n>\d+)`, "5") ``,
`sign` is `nil`. Since `capture` gives `nil` when nothing matches, `?.` and
`??` combine with it naturally:

```
let m = regex.capture(re`^(?P<verb>\S+) (?P<path>\S+) HTTP/(?P<ver>.+)$`, request);
m?.path ?? "unknown"
```

## list

Turned on with `ListBuiltins()`, used as `list.*`.

| Function | What it does | Example |
|---|---|---|
| `all(list, fn)` | Whether every item matches. | `all([2, 4, 6], x => x % 2 == 0)` → `true` |
| `any(list, fn)` | Whether at least one item matches. | `any([1, 3, 5], x => x % 2 == 0)` → `false` |
| `none(list, fn)` | Whether no item matches. | `none([1, 3, 5], x => x % 2 == 0)` → `true` |
| `one(list, fn)` | Whether exactly one item matches. | `one([1, 2, 3], x => x == 2)` → `true` |
| `count(list, fn)` | How many items match. | `count([1, 2, 3, 4], x => x > 2)` → `2` |
| `find(list, fn)` | The first item that matches, or `nil` if none do. | `find([1, 2, 3], x => x > 1)` → `2` |
| `findIndex(list, fn)` | The position of the first match (counting from 0), or `-1`. | `findIndex([1, 2, 3], x => x > 1)` → `1` |
| `findLast(list, fn)`, `findLastIndex(list, fn)` | Same as `find`/`findIndex`, but starting from the end. | `findLast([1, 2, 3, 2], x => x == 2)` → `2` |
| `groupBy(list, fn)` | Sorts items into named buckets based on what `fn` returns for each one. | `groupBy(["apple", "avocado", "banana"], x => x[0])` → `{"a": ["apple", "avocado"], "b": ["banana"]}` |
| `sort(list)` | A new list, sorted from smallest to largest (or A to Z for text). | `sort([3, 1, 2])` → `[1, 2, 3]` |
| `sortBy(list, fn)` | Sorted using a value derived from each item, rather than the item itself. | `sortBy(people, p => p.age)` sorts a list of people youngest first |
| `reverse(list)` | A new list with the items in reverse order. | `reverse([1, 2, 3])` → `[3, 2, 1]` |
| `flatten(list)` | Flattens a list of lists (however deeply nested) into one plain list. | `flatten([[1, 2], [3, [4, 5]]])` → `[1, 2, 3, 4, 5]` |
| `concat(list1, list2, ...)` | Joins any number of lists together, end to end. | `concat([1, 2], [3, 4])` → `[1, 2, 3, 4]` |
| `zip(list1, list2, ...)` | Pairs up the items at each position across two or more lists into tuples. Stops at the shortest list. | `zip([1, 2, 3], ["a", "b", "c"])` → `[[1, "a"], [2, "b"], [3, "c"]]` |
| `uniq(list)` | A new list with duplicate items removed, keeping the first of each. | `uniq([1, 2, 2, 3, 1])` → `[1, 2, 3]` |
| `keys(map)` | The map's keys, as a list. | `keys({a: 1, b: 2})` → `["a", "b"]` (in no particular order) |
| `values(map)` | The map's values, as a list. | `values({a: 1, b: 2})` → `[1, 2]` (in no particular order) |
| `mean(list)` | The average of a list of numbers. | `mean([1, 2, 3])` → `2` |
| `median(list)` | The middle value once the list is sorted. | `median([1, 3, 2])` → `2` |

One thing worth knowing: like the `/` operator, dividing two whole numbers
always rounds down rather than giving you a decimal — so `mean([1, 2, 4])`
is `2`, not `2.33`. If you need the precise answer, convert your numbers to
decimals first (see [decimal](#decimal)) or to `float64`.

There's no `take(list, n)` function — use `list[:n]` instead (see
LANGUAGE.md's section on indexing).

## time

Turned on with `TimeBuiltins()`, used as `time.*`.

This pack works with two kinds of value: a **time** is a specific point in
time (like "15 June 2024, 10:00am"), and a **duration** is a length of time
(like "2 hours"). You can subtract one time from another to get a duration
(`endTime - startTime`), and add or subtract a duration from a time
(`meetingStart + hours(1)`).

| Function | What it does | Example |
|---|---|---|
| `now()` | The current date and time. | `now()` |
| `duration(s)` | Turns text describing a length of time into a duration. | `duration("1h30m")` → one hour thirty minutes |
| `hours(n)`, `minutes(n)`, `seconds(n)`, `milliseconds(n)`, `days(n)` | Builds a duration from a number: 2 hours, 30 minutes, and so on. A day here is always exactly 24 hours. | `now() + hours(2)` → two hours from now |
| `date(s)` | Reads a date/time written in the common `2024-06-15T10:00:00Z` style. | `date("2024-06-15T10:00:00Z")` |
| `date(s, layout)` | Reads a date/time written in a different, specific layout, when your data isn't in the style above. | — |
| `date(s, layout, timezoneName)` | Same, but also says which timezone those clock numbers belong to (e.g. `"America/New_York"`). | — |
| `parse(s, format)` | Reads a date/time using a `%`-style format (see below) — usually easier than `date`'s layouts. | `parse("15/Jun/2024:10:07:30 -0700", "%d/%b/%Y:%H:%M:%S %z")` |
| `parse(s, format, timezoneName)` | Same, but says which timezone the clock numbers belong to when the text doesn't include an offset. | `parse("2024-06-15 10:00", "%Y-%m-%d %H:%M", "Europe/London")` |
| `format(t, format)` | Writes a time as text using a `%`-style format. | `format(t, "%Y-%m-%dT%H")` → `"2024-06-15T10"` |
| `timezone(t, timezoneName)` | Shows the same moment in time, but with its clock numbers adjusted for a different timezone. | `timezone(t, "America/New_York")` |
| `month(t)` | The month as a number, 1 (January) to 12 (December). | `month(t) in [6, 7, 8]` |
| `weekday(t)` | The day of the week as a number, 0 (Sunday) to 6 (Saturday). | `weekday(t) == 0 or weekday(t) == 6` → weekend |
| `windows(t, size)` / `windows(t, size, slide)` / `windows(t, size, slide, offset)` | The start of every time window of length `size` that `t` falls in, oldest first, with a new window beginning every `slide` (every `size` if you leave it out, so windows don't overlap). `offset` moves where the windows begin (see below). | `windows(t, minutes(10), minutes(5))` → for 10:07, `[10:00, 10:05]` |

You can compare times and durations directly with `==`, `<`, `>`, and so
on. There's deliberately no way to add a plain number straight to a time
(it wouldn't be clear if you meant seconds, days, or something else) — go
through `hours(n)`/`days(n)`/etc. instead.

**Windows** always line up the same way whatever time you give — a
10-minute window starts on :00, :10, :20 and so on (counted from the start
of 1970, UTC) — so every time in the same window gets the same start. Each
start is itself a time, so the end is just `start + size`:

```
map(time.windows(ts, time.minutes(10), time.minutes(5)), w => {start: w, end: w + time.minutes(10)})
```

Because they're counted from 1 January 1970 — a Thursday, at midnight UTC —
7-day windows start on Thursdays, and day-long windows start at midnight
UTC. `offset` shifts every window start later by that much. Add 4 days to
make weeks start on Monday, or shift days to start at a local midnight:

```
time.windows(ts, time.days(7), time.days(7), time.days(4))   // weeks from Monday 00:00 UTC
time.windows(ts, time.days(1), time.days(1), time.hours(5))  // days from midnight at UTC-5
```

(The same counting and the same `offset` idea are used by Spark's and
Flink's window functions, so results line up with pipelines built there.)

`slide` can't be longer than `size`, and one call can give at most 10,000
windows.

**Formats for `parse` and `format`.** Each `%` code stands for one part of
the date or time; everything else is matched or written exactly as it is.

| Code | Meaning | Example |
|---|---|---|
| `%Y` / `%y` | Year, 4 digits / 2 digits | `2024` / `24` |
| `%m` / `%-m` | Month number, 2 digits / no leading zero | `06` / `6` |
| `%b` (or `%h`) / `%B` | Month name, short / full | `Jun` / `June` |
| `%d` / `%-d` / `%e` | Day of the month, 2 digits / no leading zero / leading space | `05` / `5` / ` 5` |
| `%j` | Day of the year, 3 digits | `157` |
| `%a` / `%A` | Day name, short / full | `Wed` / `Wednesday` |
| `%H` | Hour, 00–23 | `15` |
| `%I` / `%-I` | Hour, 01–12 / no leading zero — use with `%p` | `03` / `3` |
| `%p` | `AM` or `PM` | `PM` |
| `%M` / `%-M` | Minute, 2 digits / no leading zero | `04` / `4` |
| `%S` / `%-S` | Second, 2 digits / no leading zero | `09` / `9` |
| `%f` | Microseconds, 6 digits — must come straight after a `.` or `,` | `%S.%f` → `09.250000` |
| `%z` / `%:z` | Offset from UTC | `-0700` / `-07:00` |
| `%Z` | Timezone abbreviation (for reading, prefer `%z`: abbreviations are ambiguous) | `CEST` |
| `%F` | Same as `%Y-%m-%d` | `2024-06-05` |
| `%T` | Same as `%H:%M:%S` | `15:04:09` |
| `%D` | Same as `%m/%d/%y` | `06/05/24` |
| `%R` | Same as `%H:%M` | `15:04` |
| `%%` | A plain `%` | `%` |

Month and day names are read in any capitalisation. A format used with
`parse` can't contain other text that looks like part of a date — a month
name, `AM`/`PM`, or a digit — outside its `%` codes; `parse` reports an
error rather than risk misreading it. `format` has no such limit.

### Methods on times, durations, months and weekdays

As well as the `time.*` functions, times and durations have their own
**methods**, called with a dot straight after the value: `t.Year()`,
`d.Hours()`. The names start with a capital letter and are written
exactly as shown. A method that gives back more than one value, such as
`t.ISOWeek()`, gives you a list: pick the part you want with `[0]`, `[1]`,
and so on. (Your application can switch methods off. If `t.Year()` gives a
"member Year not found" error, that's what has happened; use the `time.*`
functions instead.)

**On a time** (`t` below):

| Method | What it gives you | Example (for 10:07:30 on Saturday 15 June 2024, UTC) |
|---|---|---|
| `t.Year()`, `t.Day()`, `t.Hour()`, `t.Minute()`, `t.Second()`, `t.Nanosecond()` | One part of the date or time, as a whole number. | `t.Hour()` → `10` |
| `t.YearDay()` | Day of the year, 1 to 366. | `167` |
| `t.Month()` | The month, e.g. `June` (see below). | `t.Month() == 6` → `true` |
| `t.Weekday()` | The day of the week, e.g. `Saturday` (see below). | `t.Weekday() == 6` → `true` |
| `t.Add(d)` | The time `d` later (or earlier, if `d` is negative). Same as `t + d`. | `t.Add(time.hours(1))` → 11:07:30 |
| `t.AddDate(years, months, days)` | The time moved by whole calendar years, months and days. Unlike `time.days(n)`, this follows the calendar, so adding a month to 31 January lands in early March. | `t.AddDate(0, 1, 2)` → 17 July 2024, 10:07:30 |
| `t.Sub(u)` | The duration from `u` to `t`. Same as `t - u`. | `t.Sub(u)` |
| `t.Before(u)`, `t.After(u)`, `t.Equal(u)` | `true` or `false`. `Equal` treats the same moment in two timezones as equal. | `t.Before(deadline)` |
| `t.Compare(u)` | `-1` if `t` is earlier than `u`, `0` if they're the same moment, `1` if later. | — |
| `t.IsZero()` | `true` if `t` is the empty "no time set" value. | — |
| `t.Truncate(d)`, `t.Round(d)` | `t` rounded down, or to the nearest, whole `d`, counted from the start of year 1 (UTC). | `t.Truncate(time.hours(1))` → 10:00:00 |
| `t.UTC()`, `t.Local()` | The same moment, with its clock numbers shown in UTC or in the server's own timezone. For any other timezone, use `time.timezone(t, name)`. | — |
| `t.Location().String()` | The timezone's name. | `"Europe/London"` |
| `t.Zone()` | A list of the timezone's short abbreviation and its offset from UTC in seconds. | `["BST", 3600]` for summer in London |
| `t.ZoneBounds()` | A list of when the timezone's current rule (e.g. summer time) started and when it ends. | `[31 Mar 2024 02:00 BST, 27 Oct 2024 01:00 GMT]` for summer in London |
| `t.ISOWeek()` | A list of the ISO 8601 year and week number (weeks start on Monday; week 1 contains the year's first Thursday). | `t.ISOWeek()` → `[2024, 24]`, so `t.ISOWeek()[1]` → `24` |
| `t.Date()` | A list of the year, month and day. | `[2024, June, 15]` |
| `t.Clock()` | A list of the hour, minute and second. | `[10, 7, 30]` |
| `t.IsDST()` | `true` if daylight saving time is in effect at `t`. | — |
| `t.Unix()`, `t.UnixMilli()`, `t.UnixMicro()`, `t.UnixNano()` | Seconds (or milliseconds, microseconds, nanoseconds) since the start of 1970, UTC. | `t.Unix()` → `1718446050` |
| `t.String()` | The time written out in full. For your own layout, use `time.format`. | `"2024-06-15 10:07:30 +0000 UTC"` |

**On a duration** (`d` below):

| Method | What it gives you | Example (for 1 hour 30 minutes) |
|---|---|---|
| `d.Hours()`, `d.Minutes()`, `d.Seconds()` | The whole length in that unit, as a decimal-point number. | `d.Hours()` → `1.5` |
| `d.Milliseconds()`, `d.Microseconds()`, `d.Nanoseconds()` | The whole length in that unit, as a whole number. | `d.Milliseconds()` → `5400000` |
| `d.Truncate(m)`, `d.Round(m)` | `d` rounded down, or to the nearest, whole `m`. | `d.Round(time.hours(1))` → 2 hours |
| `d.Abs()` | `d` without its sign, so a negative duration becomes positive. | — |
| `d.String()` | The duration as text. | `"1h30m0s"` |

**On a month or a weekday** (what `t.Month()` and `t.Weekday()` give you):
`.String()` gives its English name, e.g. `t.Month().String()` → `"June"`.
You can compare them with numbers — months run 1 (January) to 12, weekdays
0 (Sunday) to 6 — but not do arithmetic on them: `t.Month() + 1` is an
error. For a number you can calculate with, use `time.month(t)` or
`time.weekday(t)`.

Things to watch out for:

- **Where a method wants a duration, give it one.** A plain number is taken
  as nanoseconds, so `t.Add(3600)` adds 3.6 millionths of a second, not an
  hour. Write `t.Add(time.hours(1))` or `t.Add(time.seconds(3600))`.
- **`t.Format(layout)` uses Go's layout style** (`"2006-01-02 15:04"`), not
  the `%` codes above. `time.format(t, "%Y-%m-%d %H:%M")` is usually easier.

## decimal

Turned on with `DecimalBuiltins()`. Unlike the other packs, its one function
is used on its own, with no `decimal.` in front: `decimal(...)`.

Ordinary computer numbers with a decimal point (`float64`) can introduce
tiny rounding errors — fine for most things, but not for money. This pack
gives you an exact decimal number instead, for calculations where every
cent needs to add up correctly.

| Function | What it does | Example |
|---|---|---|
| `decimal(x)` | Converts a whole number, decimal-point number, or piece of text into an exact decimal value. | `decimal("19.99")` |

Once you have a decimal value, ordinary arithmetic (`+ - * / %`) and
comparisons (`== < > ...`) work with it directly, and it mixes freely with
plain numbers and numeric text: `price * quantity`, `"5" + fee`, and
`amount > 100` all work as you'd expect, whether `amount`/`price`/`fee` came
from `decimal(...)` or straight from your data. `abs()`, `ceil()`,
`floor()`, and `round()` all work on decimal values too. To turn a decimal
back into an ordinary number, use `int(x)` or `float64(x)`.

### Methods on decimals

Decimal values also have **methods**, called with a dot straight after the
value: `price.Round(2)`. The names start with a capital letter and are
written exactly as shown. For everyday arithmetic and comparisons the
operators (`+`, `<`, ...) are simpler; the methods are mostly useful for
rounding and for turning a decimal into text. As with times, a method
that gives back more than one value gives you a list. (Your application
can switch methods off, in which case calling one gives a "member ... not
found" error.)

In the examples, `d` is `decimal("12.345")`.

| Method | What it gives you | Example |
|---|---|---|
| `d.Round(places)` | Rounded to `places` digits after the decimal point, halves rounding away from zero. A negative `places` rounds to tens, hundreds, and so on. | `d.Round(2)` → `12.35` |
| `d.RoundBank(places)` | Same, but halves round to the nearest even digit ("banker's rounding"). | `d.RoundBank(2)` → `12.34` |
| `d.RoundUp(places)`, `d.RoundDown(places)` | Rounded away from zero / towards zero. | `d.RoundUp(1)` → `12.4` |
| `d.RoundCeil(places)`, `d.RoundFloor(places)` | Rounded up / down (towards positive / negative infinity). | `d.RoundFloor(1)` → `12.3` |
| `d.RoundCash(interval)` | Rounded to a cash interval in hundredths: `5`, `10`, `25`, `50` or `100`. | `d.RoundCash(5)` → `12.35` |
| `d.Truncate(places)` | Cut off after `places` digits, with no rounding. | `d.Truncate(1)` → `12.3` |
| `d.Floor()`, `d.Ceil()` | Rounded down / up to a whole number. | `d.Ceil()` → `13` |
| `d.StringFixed(places)` | Rounded and written as text with exactly `places` digits after the point, adding zeros if needed. `d.StringFixedBank(places)` and `d.StringFixedCash(interval)` round as `RoundBank` and `RoundCash` do. | `decimal("5").StringFixed(2)` → `"5.00"` |
| `d.String()` | The decimal as text. | `"12.345"` |
| `d.IntPart()` | The whole-number part, with the fraction dropped. | `12` |
| `d.InexactFloat64()` | An ordinary decimal-point number (may be slightly inexact). Same as `float64(d)`. | `12.345` |
| `d.Float64()` | A list of that same number and whether it's exactly equal to `d`. | `[12.345, false]` |
| `d.Neg()`, `d.Abs()` | Sign flipped / sign removed. | `d.Neg()` → `-12.345` |
| `d.Sign()` | `-1`, `0` or `1`, depending on whether `d` is negative, zero or positive. | `1` |
| `d.IsPositive()`, `d.IsNegative()`, `d.IsZero()`, `d.IsInteger()` | `true` or `false`. | `d.IsInteger()` → `false` |
| `d.Add(x)`, `d.Sub(x)`, `d.Mul(x)`, `d.Div(x)`, `d.Mod(x)` | The same as `+ - * / %`. | `d.Mul(decimal(2))` → `24.69` |
| `d.DivRound(x, places)` | Divided by `x`, rounded to `places` digits. | `d.DivRound(decimal(3), 2)` → `4.12` |
| `d.QuoRem(x, places)` | A list of the quotient, cut off after `places` digits, and what's left over. | `d.QuoRem(decimal(2), 0)` → `[6, 0.345]` |
| `d.Pow(x)`, `d.PowInt32(n)` | `d` raised to the power `x` (a decimal) or `n` (a whole number). | `d.PowInt32(2)` → `152.399025` |
| `d.Equal(x)`, `d.LessThan(x)`, `d.LessThanOrEqual(x)`, `d.GreaterThan(x)`, `d.GreaterThanOrEqual(x)` | The same as `== < <= > >=`. | — |
| `d.Cmp(x)` | `-1`, `0` or `1`, depending on whether `d` is less than, equal to, or greater than `x`. | — |
| `d.Shift(n)` | Moves the decimal point `n` places right (or left, if `n` is negative), i.e. multiplies by 10ⁿ. | `d.Shift(2)` → `1234.5` |
| `d.Exponent()`, `d.CoefficientInt64()`, `d.NumDigits()` | How the value is stored: `d` equals `CoefficientInt64() × 10^Exponent()`, and `NumDigits()` counts the coefficient's digits. | `-3`, `12345`, `5` |

Things to watch out for:

- **Where a method wants a decimal, give it one.** `d.Add(1)` and
  `d.GreaterThan(1)` are errors; write `d.Add(decimal(1))`, or use the
  operator instead: `d + 1`, `d > 1`.

## bytes

Turned on with `ByteBuiltins()`, used as `bytes.*`.

This pack is for working with raw binary data — the kind of thing you'd
encounter reading a file format or a network message byte by byte, rather
than ordinary text (which belongs in [string](#string) instead). It's the
most technical pack here; skip it unless you're specifically working with
binary data.

A **byte** is a single whole number from 0 to 255. A **buffer** is a
sequence of bytes, similar to a list.

| Function | What it does | Example |
|---|---|---|
| `byte(x)` | Converts a number to a single byte. Numbers outside 0–255 wrap around rather than erroring. | `byte(300)` → `44` |
| `fromHex(s)` | Reads a 2-character "hex" pair (a common way of writing one byte as text, e.g. from a color code) as a byte. | `fromHex("ff")` → `255` |
| `bufFromHex(s)` | Reads a longer hex string as a whole buffer. | `bufFromHex("48656c6c6f")` → the bytes that spell "Hello" |
| `toHex(b)` | Writes one byte as a 2-character hex pair. | `toHex(255)` → `"ff"` |
| `bufToHex(buf)` | Writes a whole buffer as hex text. | — |
| `toBase64(buf)` / `fromBase64(s)` | Converts arbitrary binary data to and from base64 text — like `string.toBase64`/`fromBase64`, but for data that isn't valid text. | — |
| `bufToString(buf)` / `bufToString(buf, encoding)` | Reads a buffer as text, in `"utf-8"` unless you name a different encoding (see below). Gives you an error rather than garbled text if the buffer doesn't actually hold valid text in that encoding. | `bufToString(bufFromHex("48656c6c6f"))` → `"Hello"` |
| `bufFromString(s)` / `bufFromString(s, encoding)` | The reverse: turns text back into its raw bytes, in `"utf-8"` unless you name a different encoding. | `bufFromString("Hello")` → the bytes that spell "Hello" |
| `bitAnd(a, b)`, `bitOr(a, b)`, `bitXor(a, b)`, `bitNot(a)` | Combines two bytes bit by bit (see [bits](#bits) for what this means), or flips every bit in one byte. | `bitAnd(12, 10)` → `8` |
| `shiftLeft(a, n)`, `shiftRight(a, n)` | Shifts a byte's bits left or right by `n` places. | `shiftLeft(1, 3)` → `8` |
| `bitTest(a, idx)` | Whether bit number `idx` (0 to 7) is on. | `bitTest(5, 0)` → `true` |
| `bitSet(a, idx)`, `bitClear(a, idx)` | Turns bit number `idx` on or off. | `bitSet(0, 3)` → `8` |
| `popCount(a)` | How many bits are turned on. | `popCount(7)` → `3` |
| `concatBuf(buf1, buf2, ...)` | Joins buffers together, like `list.concat` but for binary data. | — |
| `reverseBuf(buf)` | Reverses a buffer's bytes. | — |
| `padBufStart(buf, len)`, `padBufEnd(buf, len)` | Pads a buffer with zero bytes until it's `len` bytes long. | — |
| `truncateBuf(buf, n)` | Cuts a buffer down to at most `n` bytes. | — |
| `readUint16BE(buf, offset)`, `readUint32BE(buf, offset)`, `readUint64BE(buf, offset)` | Reads a 2/4/8-byte number out of a buffer starting at position `offset`. | — |
| `readUint16LE(buf, offset)`, `readUint32LE(buf, offset)`, `readUint64LE(buf, offset)` | Same, but reading the bytes in the opposite order (this only matters when matching an exact binary format spec that calls for it). | — |
| `writeUint16BE(buf, offset, value)` and the `LE`/32/64-bit equivalents | Writes a number into a (copy of a) buffer at position `offset`. | — |

`bufToString`/`bufFromString` default to `"utf-8"` (which covers essentially
all modern text, in any language) if you don't give an encoding name, but
they're not limited to that — they accept any of the standard encoding
names used across the industry (the same ones a web page or an email
declares its text as), including the older, region-specific ones still
found in some legacy systems and exports: `"iso-8859-1"` (Western European,
also called `"latin1"`), `"windows-1252"` (its close Windows cousin),
`"Shift_JIS"`/`"EUC-JP"` (Japanese), `"GBK"`/`"GB18030"` (Chinese),
`"EUC-KR"` (Korean), `"KOI8-R"` (Russian), and many more. Names are matched
without regard to case. An older, single-region encoding can't represent
every possible character (there's no emoji in 1987!) — converting text
into one is a clean error if it contains a character that encoding simply
has no way to write down.

## iter

Turned on with `IterBuiltins()`, used as `iter.*`.

Everyday work belongs with the [list](#list) functions above. This pack
exists for one specific situation: working through a very large amount of
data — more than you'd want to hold in memory all at once — a piece at a
time. If that doesn't describe what you're doing, you probably don't need
this pack.

The functions below build up a series of steps first, and only actually
start working through the data once you ask for a final answer (with
`toList`, `count`, `first`, and similar). One consequence: if a problem
occurs partway through (say, dividing by a value that turns out to be
zero), the error shows up when you ask for that final answer, not at the
step that will eventually cause it.

| Function | What it does | Example |
|---|---|---|
| `of(list)` | Starts a pipeline from a list. | `iter.of(records)` |
| `keys(map)`, `values(map)`, `entries(map)` | Starts a pipeline from a map's keys, values, or key/value pairs. | — |
| `map(source, fn)` | Transforms each item as the pipeline runs. | `iter.map(source, x => x.price)` |
| `filter(source, fn)` | Keeps only the items that match, as the pipeline runs. | `iter.filter(source, x => x.active)` |
| `take(source, n)` | Stops after the first `n` items. | `iter.take(source, 10)` |
| `skip(source, n)` | Skips the first `n` items. | — |
| `toList(source)` | Runs the pipeline and collects every result into a list. | — |
| `toMap(source)` | Runs the pipeline and collects the results into a map. | — |
| `reduce(source, fn, initial)` | Runs the pipeline, combining every item into a single result (same idea as core `reduce`). | — |
| `count(source)` | Runs the pipeline and counts the results. | — |
| `first(source)` | Takes just the first item, or `nil` if there isn't one. | — |
| `find(source, fn)` | Finds the first matching item, stopping as soon as it's found. | — |
| `any(source, fn)`, `all(source, fn)`, `none(source, fn)` | Same idea as the `list.*` versions, stopping as early as possible. | — |
| `contains(needle, source)` | Whether `needle` turns up anywhere in the pipeline — `in`'s equivalent for this pack (it's not called `in` because that word is already part of the language itself). | `iter.contains(5, source)` |

## json

Turned on with `JSONBuiltins()`, used as `json.*`.

JSON is a common plain-text format for exchanging data between systems.
This pack converts between owlexpr's own values and JSON text.

| Function | What it does | Example |
|---|---|---|
| `toJSON(x)` | Converts a value into JSON text. | `toJSON({name: "Bob", age: 42})` → `'{"age":42,"name":"Bob"}'` |
| `fromJSON(s)` | Reads JSON text back into a value. | `fromJSON('{"name":"Bob"}')` → `{"name": "Bob"}` |

One thing worth knowing: `fromJSON` always reads numbers back using the
same number type as `3.14`, even when the JSON text looks like a whole
number — so `fromJSON("42")` behaves like `42.0`, not like the whole number
`42`, and things like `type(fromJSON("42"))` will say so even though it
prints the same. Use `int64(x)` afterward if you need a true whole number.

## bits

Turned on with `BitsBuiltins()`, used as `bits.*`.

Sometimes a single whole number is used to pack in several yes/no settings
at once — a permissions setting, or a set of feature flags, where each
individual "bit" of the number is its own on/off switch. This pack works
with those bits directly. It's the same idea as the bit-related functions
in [bytes](#bytes), but for a whole number (0 to 63 flags) rather than a
single byte (0 to 7 flags) — use whichever matches the size of the value
you're actually working with.

| Function | What it does | Example |
|---|---|---|
| `bitAnd(a, b)`, `bitOr(a, b)`, `bitXor(a, b)` | Combines two numbers bit by bit. | `bits.bitAnd(12, 10)` → `8` |
| `bitNot(a)` | Flips every bit. | — |
| `shiftLeft(a, n)`, `shiftRight(a, n)` | Shifts every bit left or right by `n` places. | `bits.shiftRight(-8, 1)` → `-4` (a negative number stays negative) |
| `bitTest(a, idx)` | Whether bit number `idx` (0 to 63) is on. | — |
| `bitSet(a, idx)`, `bitClear(a, idx)` | Turns bit number `idx` on or off. | — |
| `popCount(a)` | How many bits are turned on. | `bits.popCount(7)` → `3` |
