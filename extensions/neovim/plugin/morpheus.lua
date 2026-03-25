vim.cmd([[
  lua require('morpheus').setup()

  " User commands
  command! -nargs=0 MorpheusChat lua require('morpheus.commands').chat()
  command! -nargs=? MorpheusChatWith lua require('morpheus.commands').chat_with(<q-args>)
  command! -nargs=? MorpheusPlan lua require('morpheus.commands').plan(<q-args>)
  command! -nargs=? MorpheusExplain lua require('morpheus.commands').explain(<q-args>)
  command! -nargs=0 MorpheusExplainVisual lua require('morpheus.commands').explain_visual()
  command! -nargs=? MorpheusRefactor lua require('morpheus.commands').refactor(<q-args>)
  command! -nargs=0 MorpheusRefactorVisual lua require('morpheus.commands').refactor_visual()
  command! -nargs=0 MorpheusReview lua require('morpheus.commands').review()
  command! -nargs=0 MorpheusTest lua require('morpheus.commands').test()
  command! -nargs=0 MorpheusSkills lua require('morpheus.commands').skills()
  command! -nargs=0 MorpheusStatus lua require('morpheus.commands').status()
  command! -nargs=0 MorpheusStop lua require('morpheus.commands').stop()
]])
