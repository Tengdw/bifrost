// Fictional account equity in USD cents. External flows occur before the day's P&L.
export const DAY=86400000, start=Date.UTC(2026,7,19), end=Date.UTC(2026,10,16)
export const names=['主账号','策略账号']
export const mix=[[.47,.095,.31,.125],[.32,.08,.13,.47]]
export const date=(n:number)=>new Date(n).toISOString().slice(0,10)
export const accounts=names.map((_,j)=>{
 let balance=[7500000,2850000][j],seed=354+j*248
 return Array.from({length:Math.round((end-start)/DAY)+1},(_,i)=>{
  seed=seed*48271%2147483647
  const open=balance,flow=(i===69?(j===0?320000:0):i===78?(j===1?-110000:0):i===42?125000:0)
  const pnl=Math.round((open+flow)*(.0015+.003*Math.sin(i*.52+j)+.016*(seed/2147483647-.46)))
  balance+=flow+pnl
  return {day:start+i*DAY,open,flow,pnl,close:balance}
 })
})
export const series=(ids:number[])=>accounts[0].map((a,i)=>{
 const sum=(k:'open'|'pnl'|'flow'|'close')=>ids.reduce((v,j)=>v+accounts[j][i][k],0)
 const open=sum('open'),flow=sum('flow'),pnl=sum('pnl'),close=sum('close')
 return {day:a.day,open,flow,pnl,close,delta:close-open,rate:pnl/(open+flow)}
})
