import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import admin from './admin'
import misc from './misc'
import provider from './provider'

export default {
  ...landing,
  ...common,
  ...dashboard,
  admin,
  provider,
  ...misc,
}
