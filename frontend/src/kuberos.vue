<template>
  <div id="kuberos">
    <b-alert v-if="error" show variant="danger">
      <p>Could not connect to authentication API: {{error.response.status}} {{error.response.statusText}}</p>
    </b-alert>
    <b-container v-else fluid>
      <b-row><br /></b-row>
      <b-row>
        <b-col></b-col>
        <b-col md="10">
          <b-jumbotron>
            <template slot="header">Kuberos</template>
            <template slot="lead">
              Save <a :href="templateURL()">this file</a> as <code>~/.kube/config</code>
              to enable OIDC based <code>kubectl</code> authentication.
            </template>
          </b-jumbotron>
        </b-col>
        <b-col></b-col>
      </b-row>

      <b-row>
        <b-col></b-col>
        <b-col md="10">
          <h3>Running <code>kubectl</code></h3>
          <p>
            Once you've saved the above <code>~/.kube/config</code> file you should be
            able to run <code>kubectl</code>. For example:
          </p>
          <pre v-highlightjs><code class="bash">
          $ kubectl config get-contexts
          CURRENT   NAME          CLUSTER                    AUTHINFO   NAMESPACE
                    experimental  experimental.example.org   kuberos
                    prod          prod.example.org           kuberos

          $ kubectl --context experimental get namespaces
          NAME          STATUS    AGE
          default       Active    83d
          experimental  Active    15d

          $ kubectl --context experimental -n experimental get pods
          NAME                                       READY     STATUS             RESTARTS   AGE
          kuberos-4074452424-06m0b                   1/1       Running            1          6d
          </code></pre>
          <h3>Authenticate Manually</h3>
          <p>
            Alternately if you want to maintain your existing config you can run
            this command to add your user to <code>kubectl</code>:
          </p>
          <pre v-highlightjs><code class="bash">
          $ kubectl config set-credentials "{{kubecfg.email}}" \
            --auth-provider=oidc \
            --auth-provider-arg=client-id="{{kubecfg.clientID}}" \
            --auth-provider-arg=client-secret="{{kubecfg.clientSecret}}" \
            --auth-provider-arg=id-token="{{kubecfg.idToken}}" \
            --auth-provider-arg=refresh-token="{{kubecfg.refreshToken}}" \
            --auth-provider-arg=idp-issuer-url="{{kubecfg.issuer}}"
          </code></pre>
          Then this one to associate your user with an existing cluster:
          <pre v-highlightjs><code class="bash">
          $ kubectl config set-context mycontext \
            --cluster mycluster \
            --user="{{kubecfg.email}}"
          </code></pre>
        </b-col>
        <b-col></b-col>
      </b-row>
    </b-container>
  </div>
</template>

<script>
export default {
  name: 'kuberos',
  metaInfo: {
    title: "Kubernetes Authentication",
    htmlAttrs: {
      lang: "en"
    }
  },
  data: function() {
    return {
      error: null,
      kubecfg: {
        email: "",
        clientID: "",
        clientSecret: "",
        idToken: "",
        refreshToken: "",
        issuer: ""
      }
    }
  },
  methods: {
    'templateURL': function() {
      return "/kubecfg.yaml?" + $.param(this.kubecfg);
    }
  },
  created: function() {
    var q = decodeURI(location.search.substr(1))
      .replace(/"/g, '\\"')
      .replace(/&/g, '","')
      .replace(/=/g,'":"');
    var query = "";
    if (q != "") {
      query = JSON.parse('{"' + q + '"}');
    }
    var url = "/kubecfg?" + $.param(query);

    var _this = this;
    this.axios.get(url).then(function(response) {
      _this.kubecfg = response.data;
    }).catch(function(error) {
      _this.error = error;
    })
  }
}
</script>
