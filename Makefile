.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical

FRONTEND_CRITICAL_VITEST := \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/components/account/__tests__/accountCapacity.guard.spec.ts \
	src/components/account/__tests__/capacityBuilder.spec.ts \
	src/components/account/__tests__/CreateAccountModal.spec.ts \
	src/components/account/__tests__/CreateAccountModal.capacity.spec.ts \
	src/components/account/__tests__/EditAccountModal.spec.ts \
	src/components/account/__tests__/EditAccountModal.capacity.spec.ts \
	src/components/account/__tests__/BulkEditAccountModal.spec.ts \
	src/components/account/__tests__/BulkEditAccountModal.capacity.spec.ts \
	src/components/account/__tests__/AccountCapacityCell.spec.ts \
	src/components/admin/provider/__tests__/ProviderSettingsPanel.spec.ts \
	src/views/admin/settings/__tests__/CapacityPresetsCard.spec.ts \
	src/composables/__tests__/useCapacityPresets.spec.ts \
	src/i18n/__tests__/capacityI18nKeys.spec.ts \
	src/i18n/__tests__/messageCompilation.spec.ts \
	src/i18n/__tests__/adminLargeFilesI18nKeys.spec.ts \
	src/views/provider/__tests__/ProviderOnboard.spec.ts \
	src/views/public/__tests__/PoolHealthView.spec.ts \
	src/api/__tests__/poolHealth.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)
