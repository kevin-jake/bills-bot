# Bills

A Telegram bot that replaces the household's monthly sticky note: the list of bills to pay,
what each one came to, who settles it, and whether it is done. It also keeps the history the
sticky note threw away.

## Language

**Bill**:
A named thing the household pays every month, with the Section it is listed under and the
Payment Channel it is paid through. It is the standing list entry, not any particular month's
amount.
_Avoid_: Payee, expense, account, item

**Section**:
A free-form heading the Board groups Bills under, in a fixed display order, with a subtotal.
"BDO", "Utilities" and "Investment" are all Sections.
_Avoid_: Group, bank, category

**Payment Channel**:
Who pays a Bill and out of which account: Kevin directly, one of Sheena's accounts, or charged
to a named card. It determines whether a Bill can be Funded and which Transfer it counts toward.
_Avoid_: Method, source, payer, account

**Cycle**:
One calendar month's copy of the Bill list, created on the first of the month or on demand,
with every amount blank. Cycles overlap when an older one still has unpaid Payables.
_Avoid_: Period, billing period, batch, month on its own

**Payable**:
One Bill's obligation within one Cycle: its amount, and whether it is due, funded or paid.
_Avoid_: Due, line, item, entry, instance

**Transfer**:
The money Kevin sends into one of Sheena's accounts for a Cycle so she can pay the Bills on that
Payment Channel. It compares what was needed with what was actually sent.
_Avoid_: Deposit, remittance, top-up, settlement

**Board**:
The single pinned message in the group that shows a Cycle laid out like the sticky note, edited
in place as Payables change.
_Avoid_: List, dashboard, status message, sticky note

## Payable states

**Unknown amount**:
A Payable whose amount has not been entered yet. Not the same as zero: an unknown amount keeps
its Transfer tentative, while a zero amount means nothing is due.
_Avoid_: Blank, empty, TBD, null

**Funded**:
A Payable on one of Sheena's channels whose Transfer has been sent, but which Sheena has not yet
paid.
_Avoid_: Pending, transferred, ready

**Paid**:
A Payable that has been settled with the payee, or whose charge has been confirmed on a card
statement. Paid is a single flip; there are no partial payments.
_Avoid_: Done, settled, cleared, completed

## Money

**To settle**:
What is left to pay in a Cycle. An operational figure for working through the list, not a
measure of spending: it excludes Bills charged to a card, whose amounts already sit inside that
card's balance.
_Avoid_: Total, grand total

**Cash out**:
The money that genuinely leaves the household in a Cycle, being the Bills Kevin pays directly.
Everything else is internal movement, because funding Sheena's accounts returns as a charge on
the card Kevin settles in cash.
_Avoid_: Spend, outflow, net total
