# Render with: vhs docs/assets/demo.tape
# Requires an already-initialized bundle in the working directory (orobox init
# takes several minutes on a real OroCommerce install, so the demo starts after
# that step rather than recording it).

Output docs/assets/demo.gif

Set FontSize 16
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "orobox up"
Enter
Sleep 5s

Type "orobox shell"
Enter
Sleep 2s
Type "exit"
Enter
Sleep 1s

Type "orobox qa"
Enter
Sleep 4s

Type "orobox test"
Enter
Sleep 4s
