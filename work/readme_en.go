package main

// readmeEn is the indicators folder's README in English.
const readmeEn = `<!-- ` + readmeVersion + ` en -->
# OpenBar custom indicators

The .pine files in this folder are OpenBar's custom indicators, written in TradingView's Pine Script (most of v5 and v6; some of v4 runs too).

- While OpenBar is open it looks at this folder every 1.5 seconds. **A file newly put here is added to the chart on its own**; one already on the chart is worked out again as soon as its file changes.
- Indicators › Custom on the top bar makes new ones, imports one from the clipboard, edits and deletes them, and adds this folder's to the chart.
- A mistake breaks nothing: the chart says which line is wrong, and the editor marks it.

## For agents

When the user asks you to write an indicator for OpenBar:

1. One indicator to a file, in this folder, named .pine, in UTF-8. **The file's name is the indicator's identity in OpenBar**: renaming it makes it another indicator (the one on the chart then reads "script file not found"). The name shown is indicator()'s first argument.
2. Start with //@version=6, and have one line indicator("Name", overlay = true or false): overlay = true draws over the bars, false in a pane of its own under them.
3. When done, wait 2 seconds and read _status.json in the same folder: one entry a file; when ok is false, error says which line and what is wrong; fix it and look again. warnings are what OpenBar skips (labels, say).
4. To change an indicator, change its file; for a new one, make a new file, and do not rename others' files.
5. Write nothing outside this folder, and leave OpenBar's settings alone.

## What works

- Declarations: indicator(title, shorttitle, overlay, format, precision); study() too.
- Inputs (shown in the indicator's settings for the user to change): input.int, input.float (minval, maxval, step, options), input.bool, input.source, input.string (a drop-down with options), input.color, and input().
- Syntax: variables, var / varip, := and += -= *= /=, if / else if / else, for i = a to b by s, while, break / continue, switch (with and without a value), if and switch as expressions, the ternary ?:, history references x[n], functions of your own (one line and several, returning [a, b] too), [a, b] = f() destructuring, type annotations (float x = na).
- Data: open high low close volume hl2 hlc3 ohlc4 hlcc4 time time_close bar_index last_bar_index, barstate.*, syminfo.mintick / ticker, timeframe.period / isintraday / isdaily…, timeframe.change("D"), time("D"), year month dayofmonth dayofweek hour minute (in the exchange's time zone: UTC for crypto, New York for US stocks, Chicago for futures).
- ta: sma ema rma wma vwma hma alma swma linreg stdev variance dev median percentrank highest lowest highestbars lowestbars range change mom roc rsi cci stoch mfi cmo tsi wpr tr atr bb bbw kc kcw macd dmi sar supertrend vwap obv accdist cum max min cross crossover crossunder rising falling barssince valuewhen pivothigh pivotlow correlation nvi pvi.
- math: abs max min avg sum sqrt pow exp log log10 round floor ceil sign sin cos tan asin acos atan todegrees toradians round_to_mintick pi e.
- Also: na nz fixnan int float bool, color.new color.rgb color.from_gradient color.r/g/b/t and colors like color.red, str.tostring str.format.
- Drawing: plot (style_line linebr stepline histogram columns circles cross area; color may differ bar by bar; linewidth, offset, display = display.none), hline, fill (two plots or two hlines), bgcolor, plotshape, plotchar, plotarrow.

## What does not

- request.security and the like, which take other markets' or intervals' data: an error.
- strategy (backtesting), library, import, type, method, array / matrix / map: an error.
- label, line, box, table, polyline: no error, but skipped, not drawn (warnings say so).
- plotcandle, plotbar, barcolor, alertcondition: skipped.

## An example

~~~
//@version=6
indicator("EMA cross", overlay = true)
fastLen = input.int(9, "Fast", minval = 1)
slowLen = input.int(21, "Slow", minval = 1)
fast = ta.ema(close, fastLen)
slow = ta.ema(close, slowLen)
plot(fast, "Fast", color = color.orange)
plot(slow, "Slow", color = color.blue)
plotshape(ta.crossover(fast, slow), "Golden cross", shape.triangleup, location.belowbar, color.green, size = size.small)
plotshape(ta.crossunder(fast, slow), "Death cross", shape.triangledown, location.abovebar, color.red, size = size.small)
~~~
`
