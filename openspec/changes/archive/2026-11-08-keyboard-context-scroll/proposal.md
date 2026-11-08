# Keyboard scrolling for context details

The category popover can overflow on a small viewport without a keyboard target for its scroll region. Make the existing named panel focusable with native autofocus so keyboard scrolling reaches lower content. A tabindex alone leaves the popover root outside native tab navigation in the tested browser.

The panel remains the same node through refresh. Focus and scroll stay with it, and native Escape returns focus to the trigger. No daemon or other frontend changes. This scope was directed by em in Hera message 1317.
