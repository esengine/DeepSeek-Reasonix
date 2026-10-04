package shellparse

// PowerShellDeleteAnalysis parses stdin as data; it never invokes the supplied script.
const PowerShellDeleteAnalysis = PowerShellDeleteFunctions + `
$source = [Console]::In.ReadToEnd()
Analyze-DeleteSource $source | ConvertTo-Json -Depth 8 -Compress
`

const PowerShellDeleteServer = PowerShellDeleteFunctions + `
while ($null -ne ($line = [Console]::In.ReadLine())) {
 try {
  $source = ConvertFrom-Json -InputObject $line
  $answer = Analyze-DeleteSource $source
  [Console]::Out.WriteLine(($answer | ConvertTo-Json -Depth 8 -Compress))
 } catch {
  [Console]::Out.WriteLine('{"host_error":true}')
 }
}
`

const PowerShellDeleteFunctions = `
$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
function Literal-Value($element) {
 $literal = Literal-Node $element
 if ($literal.known) { return $literal.value }
 return ''
}
function Literal-Node($element) {
 if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst]) { return @{known=$true; value=$element.Value} }
 if ($element -is [System.Management.Automation.Language.ExpandableStringExpressionAst] -and $element.NestedExpressions.Count -eq 0) { return @{known=$true; value=$element.Value} }
 if ($element -is [System.Management.Automation.Language.ParenExpressionAst] -and $element.Pipeline -is [System.Management.Automation.Language.PipelineAst] -and $element.Pipeline.PipelineElements.Count -eq 1 -and $element.Pipeline.PipelineElements[0] -is [System.Management.Automation.Language.CommandExpressionAst]) { return (Literal-Node $element.Pipeline.PipelineElements[0].Expression) }
 if ($element -is [System.Management.Automation.Language.BinaryExpressionAst] -and $element.Operator -eq [System.Management.Automation.Language.TokenKind]::Plus) {
  $left = Literal-Node $element.Left; $right = Literal-Node $element.Right
  if ($left.known -and $right.known) { return @{known=$true; value=$left.value + $right.value} }
 }
 return @{known=$false; value=''}
}
function Delete-Name($name) {
 if ($null -ne $name) { $name = ($name -split '[\\/]')[-1] }
 return $name -in @('Remove-Item','rm','ri','rd','rmdir','del','erase')
}
function Delete-Unsafe($node, $ast) {
 $p = $node.Parent
 while ($null -ne $p -and $p -ne $ast.EndBlock) {
  if ($p -is [System.Management.Automation.Language.IfStatementAst] -or $p -is [System.Management.Automation.Language.LoopStatementAst] -or $p -is [System.Management.Automation.Language.FunctionDefinitionAst] -or $p -is [System.Management.Automation.Language.SubExpressionAst] -or $p -is [System.Management.Automation.Language.ScriptBlockExpressionAst]) { return $true }
  $p = $p.Parent
 }
 return $false
}
function Analyze-DeleteSource($source) {
 $tokens = $null
 $parseErrors = $null
 $ast = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$parseErrors)
 if ($parseErrors.Count -ne 0) { return @{syntax_error=($parseErrors | ForEach-Object { $_.Message }) -join '; '; calls=@()} }
 $defaults = @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.VariableExpressionAst] -and $n.VariablePath.UserPath -eq 'PSDefaultParameterValues' }, $true)).Count -gt 0
 $nodes = @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.CommandAst] -or $n -is [System.Management.Automation.Language.AssignmentStatementAst] -or $n -is [System.Management.Automation.Language.InvokeMemberExpressionAst] -or $n -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true) | Sort-Object { $_.Extent.StartOffset })
 $result = @()
 $aliases = @{}
 foreach ($call in $nodes) {
  if ($call -is [System.Management.Automation.Language.AssignmentStatementAst] -or $call -is [System.Management.Automation.Language.FunctionDefinitionAst]) {
   $result += @{name=''; args=@(); unsafe=$true}; continue
  }
  if ($call -is [System.Management.Automation.Language.InvokeMemberExpressionAst]) {
   if ($call.Member.Value -in @('Delete','DeleteDirectory')) {
    if ($call.Arguments.Count -eq 0) { continue }
    $path = ''
    $directoryType = $call.Static -and $call.Expression -is [System.Management.Automation.Language.TypeExpressionAst] -and $call.Expression.TypeName.FullName -in @('IO.Directory','System.IO.Directory')
    if ($directoryType -and $call.Arguments.Count -eq 1) { continue }
    if ($directoryType -and $call.Arguments.Count -eq 2) {
     if ($call.Arguments[1] -is [System.Management.Automation.Language.VariableExpressionAst] -and $call.Arguments[1].VariablePath.UserPath -eq 'false') { continue }
     $path = Literal-Value $call.Arguments[0]
    }
    $result += @{name='Remove-Item'; args=@('-Recurse',$path); unsafe=(Delete-Unsafe $call $ast)}
   } else { $result += @{name=''; args=@(); unsafe=$true} }
   continue
  }
  $name = $call.GetCommandName()
  $arguments = @()
  foreach ($element in $call.CommandElements | Select-Object -Skip 1) {
   if ($element -is [System.Management.Automation.Language.CommandParameterAst]) {
    $value = '-' + $element.ParameterName
    if ($null -ne $element.Argument) { $value += ':' + $element.Argument.Extent.Text }
    $arguments += $value
   } else { $arguments += (Literal-Value $element) }
  }
  $unsafe = $false
  $parent = $call.Parent
  if ($parent -is [System.Management.Automation.Language.PipelineAst] -and $parent.PipelineElements.Count -gt 1) {
   $deletes = @($parent.PipelineElements | Where-Object { $_ -is [System.Management.Automation.Language.CommandAst] -and (Delete-Name $_.GetCommandName()) })
   if ($deletes.Count -gt 0) {
    if (-not (Delete-Name $name)) { continue }
    $literal = ''
    if ($parent.PipelineElements.Count -eq 2 -and $parent.PipelineElements[0] -is [System.Management.Automation.Language.CommandExpressionAst]) { $literal = Literal-Value $parent.PipelineElements[0].Expression }
    $arguments = @('-Recurse',$literal)
   } else { $unsafe=$true }
  }
  $p = $parent
  while ($null -ne $p -and $p -ne $ast.EndBlock) {
   if ($p -is [System.Management.Automation.Language.IfStatementAst]) {
    $conditions = @($p.Clauses | ForEach-Object { $_.Item1.FindAll({ param($n) $n -is [System.Management.Automation.Language.CommandAst] }, $true) })
    $safeCondition = $conditions.Count -eq 1 -and $conditions[0].GetCommandName() -eq 'Test-Path' -and $conditions[0].CommandElements.Count -eq 2 -and (Literal-Value $conditions[0].CommandElements[1]) -ne ''
    if ($conditions -contains $call -and $safeCondition) { $name='__condition'; break }
    if (-not $safeCondition -or $p.ElseClause -ne $null) { $unsafe=$true }
   } elseif ($p -is [System.Management.Automation.Language.LoopStatementAst] -or $p -is [System.Management.Automation.Language.FunctionDefinitionAst] -or $p -is [System.Management.Automation.Language.SubExpressionAst] -or $p -is [System.Management.Automation.Language.ScriptBlockExpressionAst]) { $unsafe=$true }
   $p = $p.Parent
  }
  if ($name -eq '__condition') { continue }
  if ($name -in @('sal','nal','Set-Alias','New-Alias')) {
   $aliasName = ''; $aliasValue = ''
   if ($arguments.Count -ge 2 -and -not $arguments[0].StartsWith('-')) { $aliasName=$arguments[0]; $aliasValue=$arguments[1] }
   for ($i=0; $i -lt $arguments.Count-1; $i++) {
    if ($arguments[$i] -eq '-Name') { $aliasName=$arguments[$i+1] }
    if ($arguments[$i] -eq '-Value') { $aliasValue=$arguments[$i+1] }
   }
   if ($aliasName -ne '' -and ($aliasValue -eq '' -or (Delete-Name $aliasValue))) { $aliases[$aliasName]=$aliasValue }
  } elseif ($null -ne $name -and $aliases.ContainsKey($name)) { $name=$aliases[$name] }
  if ($defaults -and (Delete-Name $name)) { $arguments += '-Recurse' }
  if ($name -in @('sal','nal','Set-Alias','New-Alias') -and @($arguments | Where-Object { Delete-Name $_ }).Count -gt 0) {
   $name='Remove-Item'; $arguments=@('-Recurse',''); $unsafe=$true
  }
  $result += @{name=$name; args=@($arguments); unsafe=$unsafe}
 }
 return @{standalone=$true; calls=@($result)}
}
`
