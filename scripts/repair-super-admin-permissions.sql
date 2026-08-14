-- ============================================================================
-- 补齐超级管理员(super_admin)角色的所有权限
--
-- 何时使用：
--   1) 升级后 super_admin 缺少新增的权限（如 catalog.view / system.view 等）
--   2) 生产环境关闭了 BOOTSTRAP_ADMIN，seedBaselines 不会自动同步时
--
-- 幂等：已绑定的权限不会被重复插入，可安全重复执行。
-- 注意：如果迁移给 role_permissions 表加了 deleted_at 列，请相应调整 NOT EXISTS 子句。
-- ============================================================================

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.deleted_at IS NULL
WHERE r.code = 'super_admin'
  AND r.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM role_permissions rp
    WHERE rp.role_id = r.id
      AND rp.permission_id = p.id
  );
