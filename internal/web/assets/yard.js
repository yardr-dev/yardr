// yard.js is the view's own script: every page loads it from this server,
// and nothing in it names another host. What it does now:
//
// The talk page's textarea sends on Enter, as its Send button does, and
// breaks the line on Shift+Enter, as footplate's line does, so that a person
// moving between the two never sends half a thought with the key that breaks
// a line in the other. Ctrl+Enter and Cmd+Enter, which sent before the keys
// moved, still send; with Shift or Alt held the browser's own meaning of the
// key stands, which is the line break for Shift. The form stands outside the
// page's live part, so one listener on the document holds across htmx swaps.
// requestSubmit fires the submit event, which htmx takes as it takes the
// button's. Only the Enter key's meaning moves: a pasted line break still
// breaks the line.
//
// The talk page names the keys beside its Send button, from the words in the
// hint's data-hint. The template carries the element empty and hidden, so
// where this script does not run the page names no key, which is true: there
// Enter breaks the line and only the button sends. Each send puts a fresh
// form in place of the talk, so the hint is shown again after every swap.
//
// The talk page's letters are grouped by the reader's day and clock, which
// the browser sets over the whole pane (groupLetters, below).
//
// The overview's feed is grouped by the hour, each group under a head. A
// stream carries one event at a time and knows nothing of what the page shows,
// so each fragment it brings has the head of its hour before its entry, and so
// does an older page of the feed that is put under the entries above it:
// after each swap a head that repeats the hour above it is dropped
// (dropRepeatedHours, below).
//
// A link htmx boosts that the server answers with an error page (a bead that
// is gone, a page the view refuses) would do nothing, for htmx swaps no 4xx
// or 5xx answer. The same address loaded plainly shows the error page as a
// pasted link does, so the click falls back to that (the last listener).
document.addEventListener("keydown", function (event) {
  if (event.key !== "Enter" || event.shiftKey || event.altKey) return;
  // An Enter that confirms what an input method is composing is the
  // composition's, not the form's. isComposing says so, except in Safari,
  // which ends the composition before that keydown and reports it with
  // keyCode 229.
  if (event.isComposing || event.keyCode === 229) return;
  var target = event.target;
  if (!(target instanceof HTMLTextAreaElement) || target.id !== "text") return;
  if (!target.form) return;
  event.preventDefault();
  // A held key repeats the keydown: the first sends, and the rest do nothing.
  // They are swallowed above, so none puts a line break into the emptied form
  // the send brings, and none asks that form to send, which would show its
  // validation message at every repeat.
  if (event.repeat) return;
  target.form.requestSubmit();
});

function showSendKey() {
  var hint = document.getElementById("send-keys");
  if (!hint || !hint.hidden || !hint.dataset.hint) return;
  hint.textContent = hint.dataset.hint;
  hint.hidden = false;
}
showSendKey();
document.addEventListener("htmx:afterSwap", showSendKey);

// The talk page's letters are grouped by the day and the clock of the
// reader, which only the browser knows: the server groups them by its own,
// which is the page as it reads where this script does not run, and its
// first paint. A letter reads under the head above it, its time in the
// gutter, when it is from the same sender, within five minutes of that
// letter and on the same day; a letter that opens a day carries the day's
// separator. That is talk.go's grouped, read again here in the browser's
// zone over the whole pane: the newest page and the older pages htmx has put
// above it, which no one page of the server sees across. The classes are the
// template's (head, day, differs); the times and the day's words are
// written in the browser's zone.
var WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
var MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function sameDay(a, b) {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

// The day's separator: Today, Yesterday, or the weekday with the day and the
// month (Saturday 17 Oct), as talk.go's dayWords words it.
function dayWords(day, now) {
  if (sameDay(day, now)) return "Today";
  if (sameDay(day, new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1))) return "Yesterday";
  return WEEKDAYS[day.getDay()] + " " + day.getDate() + " " + MONTHS[day.getMonth()];
}

function pad(n) {
  return n < 10 ? "0" + n : String(n);
}

function groupLetters() {
  // The pane holds the live block before the older ones in the document and
  // shows them after: the seqs are the order of the mail.
  var letters = Array.prototype.slice.call(document.querySelectorAll("#letters .letter"));
  letters.sort(function (a, b) { return a.dataset.seq - b.dataset.seq; });
  var now = new Date();
  var before = null;
  letters.forEach(function (letter) {
    var time = letter.querySelector("time");
    var at = new Date(time.getAttribute("datetime"));
    // A time that cannot be read stands on its own, as the server's page
    // has it.
    if (isNaN(at.getTime())) {
      before = null;
      return;
    }
    var who = letter.querySelector(".who").textContent;
    var head = true, day = true, differs = false;
    if (before) {
      day = !sameDay(before.at, at);
      var gap = Math.abs(Math.floor(at.getTime() / 1000) - Math.floor(before.at.getTime() / 1000));
      head = day || letter.dataset.from !== before.from || gap > 300;
      differs = !head && who !== before.who;
    }
    letter.classList.toggle("head", head);
    letter.classList.toggle("day", day);
    letter.classList.toggle("differs", differs);
    time.textContent = pad(at.getHours()) + ":" + pad(at.getMinutes());
    letter.querySelector(".day-break").textContent = day ? dayWords(at, now) : "";
    before = { at: at, from: letter.dataset.from, who: who };
  });
}
groupLetters();
document.addEventListener("htmx:afterSwap", groupLetters);

// The feed's heads: one per hour, as the server writes them (data-hour is the
// hour's key). A head whose hour is the one of the head above it is dropped,
// so a fragment that brings its own head over an entry of an hour the feed
// already shows leaves that hour's head where it was.
function dropRepeatedHours() {
  var feed = document.getElementById("feed");
  if (!feed) return;
  var hour = null;
  Array.prototype.slice.call(feed.children).forEach(function (item) {
    if (!item.classList.contains("wv-hour")) return;
    if (item.dataset.hour === hour) item.remove();
    else hour = item.dataset.hour;
  });
}
dropRepeatedHours();
document.addEventListener("htmx:afterSwap", dropRepeatedHours);

// A boosted click the server refused is loaded as a link without script is:
// the address shows its own error page, inside the same window.
document.addEventListener("htmx:responseError", function (event) {
  var info = event.detail;
  if (!info.boosted || !info.pathInfo || !info.requestConfig || info.requestConfig.verb !== "get") return;
  window.location.assign(info.pathInfo.finalRequestPath);
});
