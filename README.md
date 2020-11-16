# Migrations-Operator

A Kubernetes operator to manage database migrations or similar application setup tasks.

## Quick Start

### Install

TODO

### Usage

For the common case of running SQL migrations for a deployment, create a Migrator object:

```yaml
apiVersion: migrations.coderanger.net/v1beta1
kind: Migrator
metadata:
  name: mymigrations
spec:
  selector:
    matchLabels:
      app: myapp
  command:
  - python
  - manage.py
  - migrate
```

This will automatically run migrations on all future deployment changes.

## How It Works

The operator has three main components: the migrations controller, the waiter init container, and the injector webhook. The migrations controller watches for new pods matching its selector and if they are running a new image, it launches a Job to run the migrations as configured. The waiter init container stalls a pod from fully launching until the required migrations have been executed successfully. The injector webhook automatically adds the waiter init container to any pod that matches a Migrator object.

Put together, these three components allow relatively normal Kubernetes usage while ensuring migrations are applied in the expected way.

## Alternatives

### Migrations-Operator vs. Helm/Argo Hooks

A common choice for running database migrations is the `pre-install/upgrade` hook in both Helm and Argo-CD. This allows for ensuring that migrations succeed before the main segment of the chart or application is applied. The main frustration with this approach is you can end up having to move a lot of things into the hook. If you pod uses a Secret or ConfigMap for holding configuration data that's required for running migrations, that will have to be hook'd too. If you need a whole chart dependency to be up for migrations, it may not even be posible. Migrations-Operator solves this by lazily cloning the pod specification on the new, waiting pods.

### Migrations-Operator vs. Init Container

Another common solution for database migrations is an init container to run the migration commands. The main problem here is locking, if you run 4 replicas of your application, all 4 of those are going to try and apply your migrations in parallel. You could add some leader election code to your migrations runner, however this has to be built in at the application image level and so requires a specific solution for each application framework or toolkit. Migrations-Operator has a top-level view of the world and so can ensure for only a single job at a time is created.
