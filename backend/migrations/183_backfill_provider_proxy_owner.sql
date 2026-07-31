-- 回填迁移 182 之前建的供号商代理归属
--
-- 182 加了 proxies.provider_user_id 但没有回填：当时依据部署记录判断生产上还没有
-- 供号商上过号。实际上站点已经开启并且有供号商建过代理，那些记录只有 name 里的
-- provider-<id>- 前缀，归属列是空的。
--
-- 后果不是立刻的：auto_assignable 默认 FALSE，这些代理不会被自动分配出去。
-- 真正的风险在管理端 —— 「允许自动分配」开关是按 provider_user_id 是否为空来置灰的，
-- 归属为空就意味着管理员可以勾上它，一勾就把这家供号商自费的出口分给了别的供号商，
-- 两家账号还会共用同一个出口 IP，正是本 fork 最不能出的问题。
--
-- 只回填「前缀能解析出、且该用户确实是供号商」的记录：管理员完全可能自己起一个
-- 叫 provider-1-xxx 的代理名，把它误标成私有会让这条代理再也进不了自动分配池。

UPDATE proxies p
SET provider_user_id = sub.owner_id
FROM (
    SELECT pr.id,
           (regexp_match(pr.name, '^provider-(\d+)-'))[1]::bigint AS owner_id
    FROM proxies pr
    WHERE pr.provider_user_id IS NULL
      AND pr.name ~ '^provider-\d+-'
) AS sub
WHERE p.id = sub.id
  AND p.provider_user_id IS NULL
  AND EXISTS (
      SELECT 1 FROM users u
      WHERE u.id = sub.owner_id
        AND u.is_provider
        AND u.role <> 'admin'
  );

-- 供号商自带的代理绝不能留在自动分配池里。
-- 正常路径下建不出这种组合（service 层的 CreateProxy/UpdateProxy 都会拒绝），
-- 这里是兜底：上面刚回填的那批在回填前是「无归属」，理论上可能已被勾选过。
UPDATE proxies
SET auto_assignable = FALSE
WHERE provider_user_id IS NOT NULL
  AND auto_assignable;
