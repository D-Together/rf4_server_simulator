const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
const html=fs.readFileSync(path.join(__dirname,'web/index.html'),'utf8');

test('bait rule editor supports cascade, rotation groups and unsaved close guard',async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.RF4_TEST_BROWSER||path.join(process.env['ProgramFiles(x86)'],'Microsoft/Edge/Application/msedge.exe')});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('rf4_admin_token','fixture'));
  const page=await context.newPage(),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  let saved=null;
  await page.route('**/*',route=>{
   const url=new URL(route.request().url());
   if(url.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:html});
   if(url.pathname==='/api/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
   if(url.pathname==='/api/bait/prefs')return route.fulfill({contentType:'application/json',body:JSON.stringify({rules_enabled:true,rule_count:1,rows:[{
    fish_key:'c.roach',name_zh:'常见拟鲤',has_rule:true,source:'override',enabled:true,
    allow:['测试蚯蚓'],deny:[],bottom_only:[],condition:'',groups:[['测试蚯蚓']],
    rotate_sec:600,rotate_mode:'soft',tier3:0,tier2:1,total:1,baits:[],in_strict:true,
    rotation:{enabled:false,mode:'soft',total:1,active:-1,active_baits:[],interval_sec:600,next_switch:0}
   }]})});
   if(url.pathname==='/api/bait/categories')return route.fulfill({contentType:'application/json',body:JSON.stringify({categories:[
    {key:27,label:'饵料',count:1,children:[{sub:6,label:'测试饵',count:1}]},
    {key:17,label:'拟饵',count:0,children:[]}
   ]})});
   if(url.pathname==='/api/bait/items')return route.fulfill({contentType:'application/json',body:JSON.stringify({rows:[
    {item_id:'test_worm',name_zh:'测试蚯蚓',category:27,sub:6,sub_label:'测试饵'}
   ],total:1})});
   if(url.pathname==='/api/bait/rules'&&route.request().method()==='PUT'){
    saved=JSON.parse(route.request().postData()||'{}');
    return route.fulfill({contentType:'application/json',body:JSON.stringify({ok:true})});
   }
   return route.fulfill({contentType:'application/json',body:'{}'});
  });
  await page.goto('http://127.0.0.1:19214/#bait',{waitUntil:'networkidle'});
  await page.locator('#baitTable .bait-edit').click();
  await page.locator('#brInterval').waitFor();
  assert.equal(await page.locator('#brInterval').inputValue(),'10');
  assert.equal(await page.locator('#brMode').inputValue(),'soft');
  assert.equal(await page.getByText('轮换组 1',{exact:true}).count(),1);
  await page.getByRole('button',{name:'＋ 新增一组'}).click();
  assert.equal(await page.getByText('轮换组 2',{exact:true}).count(),1);
  await page.getByRole('button',{name:'＋ 添加饵'}).last().click();
  await page.locator('#brPickCat').waitFor();
  await page.locator('#brPickCat').selectOption('27');
  await page.locator('#brPickList .result').filter({hasText:'测试蚯蚓'}).click();
  await page.locator('#brInterval').waitFor();
  assert.ok(await page.locator('#modal .tag').filter({hasText:'测试蚯蚓'}).count()>=2);

  let guarded=false;
  page.once('dialog',async d=>{guarded=true;assert.match(d.message(),/未保存/);await d.dismiss();});
  await page.locator('#modal .section-head button').click();
  await page.waitForTimeout(100);
  assert.equal(guarded,true);
  assert.equal(await page.locator('#modalBg').evaluate(e=>e.classList.contains('on')),true);

  page.once('dialog',async d=>{assert.match(d.message(),/未保存/);await d.accept();});
  await page.locator('#modal .section-head button').click();
  await page.waitForFunction(()=>!document.getElementById('modalBg').classList.contains('on'));
  assert.equal(saved,null);
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close();}
});
